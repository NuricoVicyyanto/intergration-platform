// Package worker menjalankan antrean job: klaim, eksekusi, laporkan.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/example/controlplane/internal/domain"
	"github.com/example/controlplane/internal/meltano"
	"github.com/example/controlplane/internal/store"
)

// SecretResolver mengambil nilai kredensial dari secret manager.
//
// Worker yang me-resolve, bukan API (meltano-go-reference/13 bagian 13.9.1),
// supaya nilai kredensial tidak pernah masuk database atau antrean.
type SecretResolver interface {
	Resolve(ctx context.Context, ref string) (string, error)
}

// EnvBuilder menerjemahkan koneksi menjadi environment variable untuk connector.
//
// Nama setting berbeda antar plugin, jadi implementasi default di sini memakai
// konvensi generik Meltano: MELTANO_EXTRACT_<SETTING> dan MELTANO_LOAD_<SETTING>
// (src/meltano/core/plugin/project_plugin.py:286-302). Plugin nyata biasanya
// butuh pemetaan khusus; ganti implementasi ini sesuai kebutuhan.
type EnvBuilder interface {
	Build(kind domain.ConnectionKind, conn domain.Connection, password string) (map[string]string, error)
}

// ConventionEnvBuilder memetakan field endpoint ke nama setting yang umum.
type ConventionEnvBuilder struct{}

// Build menghasilkan environment variable dengan prefix konvensi Meltano.
func (ConventionEnvBuilder) Build(
	kind domain.ConnectionKind, conn domain.Connection, password string,
) (map[string]string, error) {
	if isFileOrAPIDriver(conn.Driver) {
		return buildFileOrAPIEnv(kind, conn, password)
	}
	prefix := "MELTANO_EXTRACT_"
	pluginPrefix := "TAP_"
	if kind == domain.KindDestination {
		prefix = "MELTANO_LOAD_"
		pluginPrefix = "TARGET_"
	}
	pluginPrefix += strings.ToUpper(string(conn.Driver)) + "_"

	env := map[string]string{}
	add := func(setting string, v any) {
		if s := scalarString(v); s != "" {
			env[prefix+strings.ToUpper(setting)] = s
			env[pluginPrefix+strings.ToUpper(setting)] = s
		}
	}
	if conn.Endpoint != nil {
		add("HOST", conn.Endpoint["host"])
		add("DATABASE", conn.Endpoint["database"])
		add("PORT", conn.Endpoint["port"])
		add("SSL_MODE", conn.Endpoint["sslmode"])
	}
	if conn.Username != "" {
		env[prefix+"USER"] = conn.Username
		env[pluginPrefix+"USER"] = conn.Username
	}
	if password != "" {
		env[prefix+"PASSWORD"] = password
		env[pluginPrefix+"PASSWORD"] = password
	}
	if kind == domain.KindDestination && conn.Username != "" {
		// MySQL, MSSQL, and Oracle targets call this setting "username",
		// while their taps use "user".
		env[prefix+"USERNAME"] = conn.Username
		env[pluginPrefix+"USERNAME"] = conn.Username
	}
	for setting, value := range conn.Options {
		add(setting, value)
	}

	schema := connectionString(conn, "schema")
	if schema == "" {
		switch conn.Driver {
		case domain.DriverMSSQL:
			schema = "dbo"
		case domain.DriverOracle:
			schema = strings.ToUpper(conn.Username)
		default:
			schema = connectionString(conn, "database")
		}
	}
	sslMode := connectionString(conn, "sslmode")

	if conn.Driver != domain.DriverPostgres {
		switch conn.Driver {
		case domain.DriverMySQL:
			if kind == domain.KindSource {
				if sslMode != "" && sslMode != "disable" {
					add("SSL", "true")
				}
				sqlAlchemyURL, err := databaseSQLAlchemyURL(
					conn, password, "mysql+pymysql", schema,
				)
				if err != nil {
					return nil, err
				}
				env[prefix+"SQLALCHEMY_URL"] = sqlAlchemyURL
				env[pluginPrefix+"SQLALCHEMY_URL"] = sqlAlchemyURL
				schemas := connectionSchemas(conn, schema)
				encoded, err := json.Marshal(schemas)
				if err != nil {
					return nil, fmt.Errorf("encode filter_schemas: %w", err)
				}
				env[prefix+"FILTER_SCHEMAS"] = string(encoded)
				env[pluginPrefix+"FILTER_SCHEMAS"] = string(encoded)
				env["MELTANO_EXTRACT__LOAD_SCHEMA"] = schema
			} else {
				add("DRIVER_NAME", "mysql")
				add("DEFAULT_TARGET_SCHEMA", schema)
				env["MELTANO_EXTRACT__LOAD_SCHEMA"] = schema
			}
		case domain.DriverMSSQL:
			if kind == domain.KindSource {
				add("DIALECT", "mssql")
				add("DRIVER_TYPE", "pymssql")
			} else {
				add("DRIVER", "pymssql")
				add("DEFAULT_TARGET_SCHEMA", schema)
				if sslMode == "disable" || sslMode == "prefer" {
					add("TRUST_SERVER_CERTIFICATE", "true")
				}
			}
			env["MELTANO_EXTRACT__LOAD_SCHEMA"] = schema
		case domain.DriverOracle:
			if kind == domain.KindSource {
				add("USER", conn.Username)
				if service := connectionString(conn, "service_name"); service != "" {
					add("SERVICE_NAME", service)
				} else if sid := connectionString(conn, "sid"); sid != "" {
					add("SID", sid)
				} else if database := connectionString(conn, "database"); database != "" {
					add("SERVICE_NAME", database)
				}
				schemas := connectionSchemas(conn, schema)
				add("FILTER_SCHEMAS", strings.Join(schemas, ","))
			} else {
				add("USERNAME", conn.Username)
				add("DRIVER_NAME", "oracle+cx_oracle")
				if database := connectionString(conn, "database"); database == "" {
					if service := connectionString(conn, "service_name"); service != "" {
						add("DATABASE", service)
					}
				}
			}
			env["MELTANO_EXTRACT__LOAD_SCHEMA"] = schema
		case domain.DriverMongoDB:
			dsn := mongoConnectionString(conn, password)
			if kind == domain.KindSource {
				add("DATABASE", connectionString(conn, "database"))
				env[pluginPrefix+"MONGODB_CONNECTION_STRING"] = dsn
				return env, nil
			}
			env[pluginPrefix+"CONNECTION_STRING"] = dsn
			add("DB_NAME", connectionString(conn, "database"))
			return env, nil
		}
		return env, nil
	}

	if schema == "" {
		schema = "public"
	}

	urlScheme := "postgresql"
	if kind == domain.KindDestination {
		// target-postgres defaults to psycopg3. The tap currently depends on
		// psycopg2, so both sides intentionally use different URL schemes.
		urlScheme = "postgresql+psycopg"
	}
	sqlAlchemyURL, err := postgresSQLAlchemyURL(conn, password, urlScheme, sslMode)
	if err != nil {
		return nil, err
	}
	env[prefix+"SQLALCHEMY_URL"] = sqlAlchemyURL
	env[pluginPrefix+"SQLALCHEMY_URL"] = sqlAlchemyURL

	if kind == domain.KindSource {
		schemas := connectionSchemas(conn, schema)
		encoded, err := json.Marshal(schemas)
		if err != nil {
			return nil, fmt.Errorf("encode filter_schemas: %w", err)
		}
		env[prefix+"FILTER_SCHEMAS"] = string(encoded)
		env[pluginPrefix+"FILTER_SCHEMAS"] = string(encoded)
		// Meltano uses this value to derive target-postgres'
		// default_target_schema when it is not set explicitly.
		env["MELTANO_EXTRACT__LOAD_SCHEMA"] = schema
		return env, nil
	}

	// A logical database copy keeps the source schema name by default. Without
	// this, target-postgres falls back to an environment-derived schema name.
	env[prefix+"DEFAULT_TARGET_SCHEMA"] = schema
	env[pluginPrefix+"DEFAULT_TARGET_SCHEMA"] = schema
	env[pluginPrefix+"SCHEMA"] = schema
	env["MELTANO_EXTRACT__LOAD_SCHEMA"] = schema
	if env[pluginPrefix+"LOAD_METHOD"] == "" {
		// Repeated full-table runs must update existing rows instead of
		// appending a duplicate copy on every execution. Tables without a
		// primary key need an explicit load strategy.
		env[prefix+"LOAD_METHOD"] = "upsert"
		env[pluginPrefix+"LOAD_METHOD"] = "upsert"
	}
	return env, nil
}

func databaseSQLAlchemyURL(
	conn domain.Connection, password, scheme, database string,
) (string, error) {
	host := connectionString(conn, "host")
	if database == "" {
		database = connectionString(conn, "database")
	}
	if host == "" || database == "" || conn.Username == "" || password == "" {
		return "", fmt.Errorf(
			"%w: koneksi %s memerlukan host, database, username, dan password",
			domain.ErrInvalid, conn.Driver,
		)
	}
	port := connectionString(conn, "port")
	if port == "" {
		port = defaultDatabasePort(conn.Driver)
	}
	u := &url.URL{
		Scheme: scheme,
		User:   url.UserPassword(conn.Username, password),
		Host:   net.JoinHostPort(host, port),
		Path:   "/" + database,
	}
	if conn.Driver == domain.DriverMySQL {
		query := u.Query()
		query.Set("charset", "utf8mb4")
		u.RawQuery = query.Encode()
	}
	return u.String(), nil
}

func defaultDatabasePort(driver domain.Driver) string {
	switch driver {
	case domain.DriverPostgres:
		return "5432"
	case domain.DriverMySQL:
		return "3306"
	case domain.DriverMSSQL:
		return "1433"
	case domain.DriverOracle:
		return "1521"
	case domain.DriverMongoDB:
		return "27017"
	default:
		return ""
	}
}

func mongoConnectionString(conn domain.Connection, password string) string {
	host := connectionString(conn, "host")
	port := connectionString(conn, "port")
	if port == "" {
		port = defaultDatabasePort(conn.Driver)
	}
	database := connectionString(conn, "database")
	sslmode := connectionString(conn, "sslmode")
	u := &url.URL{
		Scheme: "mongodb",
		Host:   net.JoinHostPort(host, port),
		Path:   "/" + database,
	}
	if conn.Username != "" {
		u.User = url.UserPassword(conn.Username, password)
	}
	query := u.Query()
	query.Set("connectTimeoutMS", "5000")
	if authSource := connectionString(conn, "auth_source"); authSource != "" {
		query.Set("authSource", authSource)
	} else if conn.Username != "" {
		query.Set("authSource", "admin")
	}
	if sslmode != "disable" {
		query.Set("tls", "true")
	}
	for key, value := range conn.Endpoint {
		if !strings.HasPrefix(key, "mongo_") {
			continue
		}
		if text := scalarString(value); text != "" {
			query.Set(strings.TrimPrefix(key, "mongo_"), text)
		}
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func connectionString(conn domain.Connection, setting string) string {
	if value := scalarString(conn.Endpoint[setting]); value != "" {
		return value
	}
	return scalarString(conn.Options[setting])
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return ""
	}
}

func connectionSchemas(conn domain.Connection, fallback string) []string {
	raw := conn.Endpoint["schemas"]
	if raw == nil {
		raw = conn.Options["schemas"]
	}
	switch schemas := raw.(type) {
	case []string:
		if len(schemas) > 0 {
			return schemas
		}
	case []any:
		out := make([]string, 0, len(schemas))
		for _, item := range schemas {
			if schema, ok := item.(string); ok && schema != "" {
				out = append(out, schema)
			}
		}
		if len(out) > 0 {
			return out
		}
	case string:
		if schemas != "" {
			if strings.HasPrefix(strings.TrimSpace(schemas), "[") {
				var out []string
				if json.Unmarshal([]byte(schemas), &out) == nil && len(out) > 0 {
					return out
				}
			}
			return []string{schemas}
		}
	}
	return []string{fallback}
}

func postgresSQLAlchemyURL(
	conn domain.Connection, password, scheme, sslMode string,
) (string, error) {
	host := connectionString(conn, "host")
	database := connectionString(conn, "database")
	if host == "" || database == "" || conn.Username == "" || password == "" {
		return "", fmt.Errorf(
			"%w: koneksi PostgreSQL memerlukan host, database, username, dan password",
			domain.ErrInvalid,
		)
	}
	port := connectionString(conn, "port")
	if port == "" {
		port = "5432"
	}
	u := &url.URL{
		Scheme: scheme,
		User:   url.UserPassword(conn.Username, password),
		Host:   net.JoinHostPort(host, port),
		Path:   "/" + database,
	}
	if sslMode != "" {
		query := u.Query()
		query.Set("sslmode", sslMode)
		u.RawQuery = query.Encode()
	}
	return u.String(), nil
}

// Config adalah parameter worker.
type Config struct {
	WorkerID     string
	TenantsRoot  string
	SystemDBURI  string
	Lease        time.Duration
	Heartbeat    time.Duration
	LogTailLines int
	PollInterval time.Duration
	ReapInterval time.Duration
}

// Worker menarik job dari store dan menjalankannya.
type Worker struct {
	Cfg     Config
	Store   store.Store
	Adapter *meltano.Adapter
	Secrets SecretResolver
	Env     EnvBuilder
	Log     *slog.Logger
}

// Run menjalankan loop sampai ctx dibatalkan.
func (w *Worker) Run(ctx context.Context) error {
	if w.Secrets == nil {
		w.Secrets = DenyAllResolver{}
	}
	if w.Env == nil {
		w.Env = ConventionEnvBuilder{}
	}
	if w.Cfg.PollInterval <= 0 {
		w.Cfg.PollInterval = 2 * time.Second
	}
	if w.Cfg.ReapInterval <= 0 {
		w.Cfg.ReapInterval = w.Cfg.Lease / 2
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.reapLoop(ctx)
	}()

	w.Log.Info("worker mulai",
		slog.String("worker_id", w.Cfg.WorkerID),
		slog.Duration("lease", w.Cfg.Lease),
		slog.Duration("heartbeat", w.Cfg.Heartbeat))

	ticker := time.NewTicker(w.Cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.Log.Info("worker berhenti")
			wg.Wait()
			return nil
		case <-ticker.C:
			if err := w.tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				w.Log.Error("siklus worker gagal", slog.Any("error", err))
			}
		}
	}
}

// tick mengklaim dan menjalankan satu job bila tersedia.
func (w *Worker) tick(ctx context.Context) error {
	run, err := w.Store.ClaimRun(ctx, w.Cfg.WorkerID, w.Cfg.Lease)
	if err != nil {
		return fmt.Errorf("claim run: %w", err)
	}
	if run == nil {
		return nil
	}
	return w.execute(ctx, *run)
}

func (w *Worker) execute(ctx context.Context, run domain.Run) error {
	log := w.Log.With(
		slog.String("run_id", run.ID),
		slog.String("pipeline_id", run.PipelineID))

	pipeline, err := w.Store.GetPipeline(ctx, run.PipelineID)
	if err != nil {
		return w.fail(ctx, run, err)
	}
	tenant, err := w.Store.GetTenant(ctx, pipeline.TenantID)
	if err != nil {
		return w.fail(ctx, run, err)
	}

	projectDir := tenant.ProjectDir
	if projectDir == "" && w.Cfg.TenantsRoot != "" {
		projectDir = filepath.Join(w.Cfg.TenantsRoot, tenant.Name)
	}
	if projectDir == "" {
		return w.fail(ctx, run, errors.New("tenant tidak punya project_dir dan CP_TENANTS_ROOT kosong"))
	}
	if _, err := os.Stat(filepath.Join(projectDir, "meltano.yml")); errors.Is(err, os.ErrNotExist) {
		if err := w.Adapter.InitProject(ctx, projectDir); err != nil {
			return w.fail(ctx, run, err)
		}
	} else if err != nil {
		return w.fail(ctx, run, fmt.Errorf("periksa project tenant %s: %w", projectDir, err))
	}

	env, stateID, err := w.buildConnectorEnv(ctx, pipeline, tenant)
	if err != nil {
		return w.fail(ctx, run, err)
	}
	if err := applySourceFilter(env, pipeline.Extractor, pipeline.SourceFilter); err != nil {
		return w.fail(ctx, run, err)
	}
	if err := applyTransferOptions(env, pipeline.Loader, pipeline.TransferOptions); err != nil {
		return w.fail(ctx, run, err)
	}

	suffix := ""
	if run.StateIDSuffix != nil {
		suffix = *run.StateIDSuffix
	}

	// --timeout milik Meltano untuk pelaporan yang rapi; context deadline
	// adalah penghenti keras kalau Meltano tidak menghormatinya.
	timeout := time.Duration(run.TimeoutSeconds) * time.Second
	runCtx, cancel := context.WithTimeout(ctx, timeout+meltano.DefaultGracePeriod)
	defer cancel()

	hbCtx, stopHB := context.WithCancel(runCtx)
	defer stopHB()
	heartbeatErr := w.startHeartbeat(hbCtx, run.ID, cancel)

	_ = w.Store.AppendEvent(ctx, domain.RunEvent{
		RunID: run.ID, Kind: "phase",
		Payload: map[string]any{"phase": "running", "worker": w.Cfg.WorkerID},
	})

	var observedLines atomic.Int64
	res, err := w.Adapter.Run(runCtx, meltano.RunRequest{
		ProjectDir:   projectDir,
		Trigger:      run.ID,
		SystemDBURI:  w.Cfg.SystemDBURI,
		ConnectorEnv: env,
		LogTailLines: w.Cfg.LogTailLines,
		OnLine: func(line string) {
			if observedLines.Load() >= 200 {
				return
			}
			parsed := meltano.ParseExecutionLine(line)
			if !meltano.IsUsefulExecutionLine(parsed) {
				return
			}
			observedLines.Add(1)
			_ = w.Store.AppendEvent(ctx, domain.RunEvent{
				RunID: run.ID,
				Kind:  "log",
				Payload: map[string]any{
					"raw": line,
				},
			})
		},
		Spec: meltano.RunSpec{
			Environment:    tenant.EnvironmentName,
			Extractor:      pipeline.Extractor,
			Loader:         pipeline.Loader,
			RunID:          run.ID,
			TimeoutSeconds: run.TimeoutSeconds,
			StateIDSuffix:  suffix,
			FullRefresh:    run.FullRefresh,
		},
	})

	// Hentikan heartbeat SEBELUM menunggunya. Kalau tidak, terjadi deadlock:
	// goroutine heartbeat hanya berhenti setelah stopHB dipanggil, sedangkan
	// defer stopHB baru berjalan ketika fungsi ini kembali.
	stopHB()
	if hbErr := <-heartbeatErr; hbErr != nil {
		log.Warn("heartbeat berhenti dengan error", slog.Any("error", hbErr))
	}

	if err != nil {
		return w.fail(ctx, run, err)
	}

	// Retry otomatis hanya untuk kegagalan yang ditandai retryable
	// (doc 14 bagian 14.2.3). Saat ini run diantrekan ulang dengan menghitung
	// auto_attempts pada run yang sama.
	status := res.Status
	autoAttempts := run.AutoAttempts
	if res.Retryable && autoAttempts+1 < pipeline.MaxAttempts {
		autoAttempts++
		status = domain.StatusQueued
		log.Info("menjadwalkan retry otomatis",
			slog.Int("auto_attempts", autoAttempts),
			slog.Int("max_attempts", pipeline.MaxAttempts))
	}

	exitCode := res.ExitCode
	updated, err := w.Store.CompleteRun(ctx, run.ID, store.Completion{
		Status:         status,
		ExitCode:       &exitCode,
		Error:          res.Reason,
		LogTail:        res.LogTail,
		MeltanoStateID: stateID,
		MeltanoRunID:   run.ID,
		AutoAttempts:   autoAttempts,
	})
	if err != nil {
		return fmt.Errorf("simpan hasil run: %w", err)
	}
	_ = w.Store.AppendEvent(ctx, domain.RunEvent{
		RunID: run.ID, Kind: "phase",
		Payload: map[string]any{
			"phase":         string(updated.Status),
			"exit_code":     exitCode,
			"duration_secs": res.Duration().Seconds(),
		},
	})
	log.Info("run selesai",
		slog.String("status", string(updated.Status)),
		slog.Int("exit_code", exitCode))
	return nil
}

// buildConnectorEnv me-resolve kredensial dan menyusun environment connector.
func (w *Worker) buildConnectorEnv(
	ctx context.Context, p domain.Pipeline, tenant domain.Tenant,
) (map[string]string, string, error) {
	out := map[string]string{}

	apply := func(kind domain.ConnectionKind, connID string) error {
		if connID == "" {
			return nil
		}
		conn, err := w.Store.GetConnection(ctx, connID)
		if err != nil {
			return fmt.Errorf("koneksi %s: %w", connID, err)
		}
		secret := conn.Password
		if secret == "" && conn.SecretRef != "" {
			var err error
			secret, err = w.Secrets.Resolve(ctx, conn.SecretRef)
			if err != nil {
				return fmt.Errorf("resolve secret %s: %w", conn.SecretRef, err)
			}
		}
		env, err := w.Env.Build(kind, conn, secret)
		if err != nil {
			return err
		}
		for k, v := range env {
			out[k] = v
		}
		return nil
	}

	if err := apply(domain.KindSource, p.SourceConnectionID); err != nil {
		return nil, "", err
	}
	if err := apply(domain.KindDestination, p.DestinationConnectionID); err != nil {
		return nil, "", err
	}

	// state_id dihitung di sini juga agar API bisa menampilkannya tanpa menjalankan job.
	stateID, err := meltano.StateID(tenant.EnvironmentName, p.Extractor, p.Loader, "")
	if err != nil {
		return nil, "", err
	}
	return out, stateID, nil
}

// startHeartbeat menjalankan heartbeat sampai ctx dibatalkan, lalu menutup channel.
func (w *Worker) startHeartbeat(
	ctx context.Context,
	runID string,
	cancelRun context.CancelFunc,
) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		heartbeatTicker := time.NewTicker(w.Cfg.Heartbeat)
		defer heartbeatTicker.Stop()
		cancelTicker := time.NewTicker(time.Second)
		defer cancelTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeatTicker.C:
				if err := w.Store.HeartbeatRun(ctx, runID, w.Cfg.WorkerID, w.Cfg.Lease); err != nil {
					errCh <- err
					return
				}
			case <-cancelTicker.C:
				run, err := w.Store.GetRun(ctx, runID)
				if err != nil {
					errCh <- err
					return
				}
				if run.Status == domain.StatusCancelling {
					w.Log.Info("permintaan pembatalan diterima; menghentikan meltano",
						slog.String("run_id", runID))
					cancelRun()
					return
				}
			}
		}
	}()
	return errCh
}

func (w *Worker) fail(ctx context.Context, run domain.Run, cause error) error {
	code := meltano.ExitFailure
	_, err := w.Store.CompleteRun(ctx, run.ID, store.Completion{
		Status:   domain.StatusFailed,
		ExitCode: &code,
		Error:    cause.Error(),
	})
	if err != nil {
		return fmt.Errorf("%v (dan gagal menyimpan status: %w)", cause, err)
	}
	w.Log.Error("run gagal sebelum atau saat spawn",
		slog.String("run_id", run.ID), slog.Any("error", cause))
	return nil
}

// reapLoop menandai run yang lease-nya habis sebagai failed.
func (w *Worker) reapLoop(ctx context.Context) {
	ticker := time.NewTicker(w.Cfg.ReapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := w.Store.ReapExpired(ctx, time.Now().UTC())
			if err != nil {
				w.Log.Error("reap gagal", slog.Any("error", err))
				continue
			}
			if n > 0 {
				w.Log.Warn("run ditandai gagal karena lease kedaluwarsa", slog.Int("count", n))
			}
		}
	}
}

// DenyAllResolver adalah resolver default yang menolak semua permintaan.
//
// Ini memaksa implementasi nyata dipasang sebelum pipeline dengan kredensial
// dijalankan, alih-alih diam-diam mengirim password kosong ke connector.
type DenyAllResolver struct{}

// Resolve selalu mengembalikan error.
func (DenyAllResolver) Resolve(_ context.Context, ref string) (string, error) {
	return "", fmt.Errorf("resolver secret belum dikonfigurasi untuk %q", ref)
}

// StaticResolver memetakan ref ke nilai tetap. Untuk test dan pengembangan lokal.
type StaticResolver map[string]string

// Resolve mengembalikan nilai yang terdaftar.
func (s StaticResolver) Resolve(_ context.Context, ref string) (string, error) {
	if v, ok := s[ref]; ok {
		return v, nil
	}
	return "", fmt.Errorf("secret %q tidak terdaftar", ref)
}

// EnvResolver membaca kredensial dari environment variable.
//
// Nama variable dibentuk dari secret_ref: semua karakter non-alfanumerik menjadi
// "_", lalu di-uppercase, dengan awalan CP_SECRET_. Contoh:
//
//	secret_ref "acme-orders-password"  ->  CP_SECRET_ACME_ORDERS_PASSWORD
//
// Resolver ini hanya untuk pengembangan dan demo. Di produksi pakai Vault atau
// Secrets Manager: nilainya harus diambil tepat saat spawn, bukan dibaca dari
// environment proses worker yang berumur panjang.
type EnvResolver struct{}

// Resolve membaca CP_SECRET_<REF> dari environment.
func (EnvResolver) Resolve(_ context.Context, ref string) (string, error) {
	key := "CP_SECRET_" + normalizeRef(ref)
	if v := os.Getenv(key); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("secret %q tidak ditemukan; set %s", ref, key)
}

func normalizeRef(ref string) string {
	var b strings.Builder
	b.Grow(len(ref))
	for _, r := range strings.ToUpper(ref) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
