// Package worker menjalankan antrean job: klaim, eksekusi, laporkan.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
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
	prefix := "MELTANO_EXTRACT_"
	if kind == domain.KindDestination {
		prefix = "MELTANO_LOAD_"
	}

	env := map[string]string{}
	add := func(setting string, v any) {
		if s, ok := v.(string); ok && s != "" {
			env[prefix+strings.ToUpper(setting)] = s
		}
	}
	if conn.Endpoint != nil {
		add("HOST", conn.Endpoint["host"])
		add("DATABASE", conn.Endpoint["database"])
		add("PORT", conn.Endpoint["port"])
	}
	if conn.Username != "" {
		env[prefix+"USER"] = conn.Username
	}
	if password != "" {
		env[prefix+"PASSWORD"] = password
	}
	return env, nil
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
		projectDir = w.Cfg.TenantsRoot + "/" + tenant.Name
	}

	env, stateID, err := w.buildConnectorEnv(ctx, pipeline, tenant)
	if err != nil {
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
	heartbeatErr := w.startHeartbeat(hbCtx, run.ID)

	_ = w.Store.AppendEvent(ctx, domain.RunEvent{
		RunID: run.ID, Kind: "phase",
		Payload: map[string]any{"phase": "running", "worker": w.Cfg.WorkerID},
	})

	res, err := w.Adapter.Run(runCtx, meltano.RunRequest{
		ProjectDir:   projectDir,
		Trigger:      run.ID,
		SystemDBURI:  w.Cfg.SystemDBURI,
		ConnectorEnv: env,
		LogTailLines: w.Cfg.LogTailLines,
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
	ctx context.Context, p domain.Pipeline, _ domain.Tenant,
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
		secret, err := w.Secrets.Resolve(ctx, conn.SecretRef)
		if err != nil {
			return fmt.Errorf("resolve secret %s: %w", conn.SecretRef, err)
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
	stateID, err := meltano.StateID("", p.Extractor, p.Loader, "")
	if err != nil {
		return nil, "", err
	}
	return out, stateID, nil
}

// startHeartbeat menjalankan heartbeat sampai ctx dibatalkan, lalu menutup channel.
func (w *Worker) startHeartbeat(ctx context.Context, runID string) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		ticker := time.NewTicker(w.Cfg.Heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := w.Store.HeartbeatRun(ctx, runID, w.Cfg.WorkerID, w.Cfg.Lease); err != nil {
					errCh <- err
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
