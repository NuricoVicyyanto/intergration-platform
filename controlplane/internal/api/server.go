// Package api menyediakan HTTP API control plane.
//
// Cakupan implementasi saat ini adalah subset dari
// meltano-go-reference/14-api-reference.md: tenant, connection, pipeline,
// run, event, dan meta. Endpoint yang belum ada tercantum di README.
package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/example/controlplane/internal/connector"
	"github.com/example/controlplane/internal/domain"
	"github.com/example/controlplane/internal/meltano"
	"github.com/example/controlplane/internal/store"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	_ "github.com/microsoft/go-mssqldb"
	goora "github.com/sijms/go-ora/v2"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Version adalah versi control plane yang dilaporkan /v1/version.
const Version = "0.1.0"

// Server merangkai handler HTTP.
type Server struct {
	store          store.Store
	log            *slog.Logger
	meltanoVersion string

	// connectors membaca plugin dari project Meltano tenant. Kosong berarti
	// mode tenant pada GET /v1/connectors tidak tersedia.
	connectors            ConnectorLister
	tenantsRoot           string
	allowPlaintextSecrets bool
	sessionDuration       time.Duration
	disableAuth           bool // hanya untuk unit test handler lama
}

// ConnectorLister membaca daftar plugin project Meltano sebuah tenant.
// Dipenuhi oleh *meltano.Adapter.
type ConnectorLister interface {
	ListPlugins(ctx context.Context, projectDir string) ([]meltano.Plugin, error)
}

// Option menyesuaikan Server tanpa mengubah pemanggil New yang sudah ada.
type Option func(*Server)

// WithConnectorLister mengaktifkan mode tenant pada GET /v1/connectors.
// tenantsRoot dipakai sebagai fallback ketika Tenant.ProjectDir kosong.
func WithConnectorLister(l ConnectorLister, tenantsRoot string) Option {
	return func(s *Server) {
		s.connectors = l
		s.tenantsRoot = tenantsRoot
	}
}

// WithPlaintextSecrets enables password input for local/demo deployments only.
func WithPlaintextSecrets(enabled bool) Option {
	return func(s *Server) { s.allowPlaintextSecrets = enabled }
}

// WithSessionDuration mengatur masa berlaku cookie session.
func WithSessionDuration(duration time.Duration) Option {
	return func(s *Server) {
		if duration > 0 {
			s.sessionDuration = duration
		}
	}
}

// New membuat Server.
func New(st store.Store, log *slog.Logger, meltanoVersion string, opts ...Option) *Server {
	s := &Server{
		store:           st,
		log:             log,
		meltanoVersion:  meltanoVersion,
		sessionDuration: 24 * time.Hour,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Handler mengembalikan router dengan seluruh route terdaftar.
//
// Pola routing memakai method + wildcard bawaan net/http (Go 1.22+),
// sehingga tidak diperlukan router eksternal.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /v1/version", s.handleVersion)
	mux.HandleFunc("POST /v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /v1/auth/me", s.handleMe)
	mux.HandleFunc("POST /v1/auth/password", s.handleChangePassword)

	mux.HandleFunc("GET /v1/users", s.handleListUsers)
	mux.HandleFunc("POST /v1/users", s.handleCreateUser)
	mux.HandleFunc("PATCH /v1/users/{userID}", s.handleUpdateUser)
	mux.HandleFunc("DELETE /v1/users/{userID}", s.handleDeleteUser)

	mux.HandleFunc("GET /v1/tenants", s.handleListTenants)
	mux.HandleFunc("POST /v1/tenants", s.handleCreateTenant)
	mux.HandleFunc("GET /v1/tenants/{tenantID}", s.handleGetTenant)
	mux.HandleFunc("PATCH /v1/tenants/{tenantID}", s.handleUpdateTenant)
	mux.HandleFunc("DELETE /v1/tenants/{tenantID}", s.handleDeleteTenant)

	mux.HandleFunc("GET /v1/connections", s.handleListConnections)
	mux.HandleFunc("POST /v1/connections", s.handleCreateConnection)
	mux.HandleFunc("POST /v1/connections/test", s.handleTestConnection)
	mux.HandleFunc("GET /v1/connections/{connectionID}", s.handleGetConnection)
	mux.HandleFunc("PATCH /v1/connections/{connectionID}", s.handleUpdateConnection)
	mux.HandleFunc("DELETE /v1/connections/{connectionID}", s.handleDeleteConnection)

	mux.HandleFunc("GET /v1/connectors", s.handleListConnectors)

	mux.HandleFunc("GET /v1/pipelines", s.handleListPipelines)
	mux.HandleFunc("POST /v1/pipelines", s.handleCreatePipeline)
	mux.HandleFunc("GET /v1/pipelines/{pipelineID}", s.handleGetPipeline)
	mux.HandleFunc("PATCH /v1/pipelines/{pipelineID}", s.handleUpdatePipeline)
	mux.HandleFunc("DELETE /v1/pipelines/{pipelineID}", s.handleDeletePipeline)
	mux.HandleFunc("POST /v1/pipelines/{pipelineID}/runs", s.handleCreateRun)
	mux.HandleFunc("POST /v1/pipelines/{pipelineID}/validate", s.handleValidatePipeline)

	mux.HandleFunc("GET /v1/runs", s.handleListRuns)
	mux.HandleFunc("GET /v1/runs/{runID}", s.handleGetRun)
	mux.HandleFunc("GET /v1/runs/{runID}/logs", s.handleGetRunLogs)
	mux.HandleFunc("POST /v1/runs/{runID}/cancel", s.handleCancelRun)
	mux.HandleFunc("POST /v1/runs/{runID}/retry", s.handleRetryRun)
	mux.HandleFunc("GET /v1/runs/{runID}/events", s.handleListEvents)

	return s.withRequestID(s.withLogging(s.withLocalCORS(s.withAuth(mux))))
}

// --- middleware ---

func (s *Server) withLocalCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173" ||
			origin == "http://localhost:4173" || origin == "http://127.0.0.1:4173" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key, X-Request-Id")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withRequestID meneruskan atau membuat X-Request-Id.
func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = strconv.FormatInt(time.Now().UnixNano(), 36)
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("http",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Duration("duration", time.Since(start)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// --- meta ---

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	// Store in-memory tidak punya ping; kehadiran store dianggap siap.
	if _, err := s.store.ListTenants(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ready": false,
			"checks": map[string]any{
				"job_store": map[string]any{"ok": false, "error": err.Error()},
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ready": true,
		"checks": map[string]any{
			"job_store": map[string]any{"ok": true},
		},
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"control_plane": Version,
		"meltano":       s.meltanoVersion,
		"api_version":   "v1",
	})
}

// --- tenant ---

func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListTenants(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "has_more": false})
}

func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name            string `json:"name"`
		DisplayName     string `json:"display_name"`
		EnvironmentName string `json:"environment_name"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeError(w, r, errors.Join(domain.ErrInvalid, errors.New("name wajib diisi")))
		return
	}
	if in.EnvironmentName == "" {
		// meltano init membuat environment default bernama dev.
		in.EnvironmentName = "dev"
	}
	t, err := s.store.CreateTenant(r.Context(), domain.Tenant{
		Name:            in.Name,
		DisplayName:     in.DisplayName,
		EnvironmentName: in.EnvironmentName,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleGetTenant(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTenant(r.Context(), r.PathValue("tenantID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleUpdateTenant(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name            *string `json:"name"`
		DisplayName     *string `json:"display_name"`
		EnvironmentName *string `json:"environment_name"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	t, err := s.store.GetTenant(r.Context(), r.PathValue("tenantID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			writeError(w, r, errors.Join(domain.ErrInvalid, errors.New("name tidak boleh kosong")))
			return
		}
		if name != t.Name && strings.TrimSpace(t.ProjectDir) == "" {
			writeError(w, r, errors.Join(domain.ErrConflict,
				errors.New("name tidak bisa diubah saat project memakai CP_TENANTS_ROOT/<name>; ubah display_name atau project_dir lebih dulu")))
			return
		}
		t.Name = name
	}
	if in.DisplayName != nil {
		t.DisplayName = strings.TrimSpace(*in.DisplayName)
	}
	if in.EnvironmentName != nil {
		t.EnvironmentName = strings.TrimSpace(*in.EnvironmentName)
	}
	if t.EnvironmentName == "" {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("environment_name tidak boleh kosong")))
		return
	}
	t, err = s.store.UpdateTenant(r.Context(), t)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTenant(r.Context(), r.PathValue("tenantID")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- connection ---

func (s *Server) handleListConnections(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListConnections(r.Context(), r.URL.Query().Get("tenant_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	// Nilai secret tidak pernah dikembalikan; secret_ref sudah berupa referensi.
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "has_more": false})
}

func (s *Server) handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TenantID  string         `json:"tenant_id"`
		Name      string         `json:"name"`
		Kind      string         `json:"kind"`
		Driver    string         `json:"driver"`
		Endpoint  map[string]any `json:"endpoint"`
		Username  string         `json:"username"`
		SecretRef string         `json:"secret_ref"`
		Password  string         `json:"password"`
		Options   map[string]any `json:"options"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Password) != "" && !s.allowPlaintextSecrets {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("password langsung hanya boleh dipakai saat CP_ALLOW_PLAINTEXT_SECRETS=true")))
		return
	}
	kind := domain.ConnectionKind(in.Kind)
	if kind != domain.KindSource && kind != domain.KindDestination {
		writeError(w, r, errors.Join(domain.ErrInvalid, errors.New("kind harus source atau destination")))
		return
	}
	driver := domain.Driver(in.Driver)
	if driver.RequiresDatabaseSecret() && !s.allowPlaintextSecrets && strings.TrimSpace(in.SecretRef) == "" {
		// Kredensial database hanya dikirim sebagai referensi.
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("secret_ref wajib diisi; nilai kredensial tidak boleh dikirim ke API")))
		return
	}
	if !driver.Valid() {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("driver harus postgres, mysql, mssql, oracle, mongodb, rest, csv, google-sheets, s3, atau duckdb")))
		return
	}
	c, err := s.store.CreateConnection(r.Context(), domain.Connection{
		TenantID:  in.TenantID,
		Name:      in.Name,
		Kind:      kind,
		Driver:    driver,
		Endpoint:  in.Endpoint,
		Username:  in.Username,
		SecretRef: in.SecretRef,
		Password:  in.Password,
		Options:   in.Options,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleGetConnection(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetConnection(r.Context(), r.PathValue("connectionID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleUpdateConnection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      *string        `json:"name"`
		Kind      *string        `json:"kind"`
		Driver    *string        `json:"driver"`
		Endpoint  map[string]any `json:"endpoint"`
		Username  *string        `json:"username"`
		SecretRef *string        `json:"secret_ref"`
		Password  *string        `json:"password"`
		Options   map[string]any `json:"options"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	c, err := s.store.GetConnection(r.Context(), r.PathValue("connectionID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	oldKind, oldDriver := c.Kind, c.Driver
	if in.Name != nil {
		c.Name = strings.TrimSpace(*in.Name)
	}
	if in.Kind != nil {
		c.Kind = domain.ConnectionKind(*in.Kind)
	}
	if in.Driver != nil {
		c.Driver = domain.Driver(*in.Driver)
	}
	if in.Endpoint != nil {
		c.Endpoint = in.Endpoint
	}
	if in.Username != nil {
		c.Username = strings.TrimSpace(*in.Username)
	}
	if in.SecretRef != nil {
		c.SecretRef = strings.TrimSpace(*in.SecretRef)
	}
	if in.Password != nil && *in.Password != "" {
		if !s.allowPlaintextSecrets {
			writeError(w, r, errors.Join(domain.ErrInvalid,
				errors.New("password langsung hanya boleh dipakai saat CP_ALLOW_PLAINTEXT_SECRETS=true")))
			return
		}
		c.Password = *in.Password
	}
	if in.Options != nil {
		c.Options = in.Options
	}
	if strings.TrimSpace(c.Name) == "" {
		writeError(w, r, errors.Join(domain.ErrInvalid, errors.New("name wajib diisi")))
		return
	}
	if c.Kind != domain.KindSource && c.Kind != domain.KindDestination {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("kind harus source atau destination")))
		return
	}
	if !c.Driver.Valid() {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("driver harus postgres, mysql, mssql, oracle, mongodb, rest, csv, google-sheets, s3, atau duckdb")))
		return
	}
	if c.Driver.RequiresDatabaseSecret() && c.SecretRef == "" && c.Password == "" {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("secret_ref atau password wajib diisi")))
		return
	}
	if c.Kind != oldKind || c.Driver != oldDriver {
		pipelines, err := s.store.ListPipelines(r.Context(), c.TenantID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		for _, pipeline := range pipelines {
			if pipeline.SourceConnectionID == c.ID || pipeline.DestinationConnectionID == c.ID {
				writeError(w, r, errors.Join(domain.ErrConflict,
					errors.New("kind/driver koneksi yang masih dipakai pipeline tidak boleh diubah")))
				return
			}
		}
	}
	c, err = s.store.UpdateConnection(r.Context(), c)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteConnection(r.Context(), r.PathValue("connectionID")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Driver   string         `json:"driver"`
		Endpoint map[string]any `json:"endpoint"`
		Username string         `json:"username"`
		Password string         `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.allowPlaintextSecrets {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("uji koneksi dengan password langsung memerlukan CP_ALLOW_PLAINTEXT_SECRETS=true")))
		return
	}
	driver := domain.Driver(in.Driver)
	if !driver.Valid() {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("driver harus postgres, mysql, mssql, oracle, mongodb, rest, csv, google-sheets, s3, atau duckdb")))
		return
	}
	host, _ := in.Endpoint["host"].(string)
	port, _ := in.Endpoint["port"].(string)
	database, _ := in.Endpoint["database"].(string)
	if host == "" || port == "" || in.Username == "" || in.Password == "" {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("host, port, username, dan password wajib diisi")))
		return
	}
	sslmode, _ := in.Endpoint["sslmode"].(string)
	if sslmode == "" {
		sslmode = "prefer"
	}
	// Fail fast for an unreachable host; a real database should authenticate
	// well within this window.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var err error
	switch driver {
	case domain.DriverPostgres:
		if database == "" {
			writeError(w, r, errors.Join(domain.ErrInvalid,
				errors.New("database wajib diisi untuk PostgreSQL")))
			return
		}
		dsn := "postgres://" + url.QueryEscape(in.Username) + ":" + url.QueryEscape(in.Password) +
			"@" + net.JoinHostPort(host, port) + "/" + url.PathEscape(database) +
			"?sslmode=" + url.QueryEscape(sslmode)
		var config *pgx.ConnConfig
		config, err = pgx.ParseConfig(dsn)
		if err == nil {
			var conn *pgx.Conn
			conn, err = pgx.ConnectConfig(ctx, config)
			if err == nil {
				defer conn.Close(context.Background())
				err = conn.Ping(ctx)
			}
		}
	case domain.DriverMySQL:
		if database == "" {
			writeError(w, r, errors.Join(domain.ErrInvalid,
				errors.New("database wajib diisi untuk MySQL")))
			return
		}
		cfg := mysql.NewConfig()
		cfg.User = in.Username
		cfg.Passwd = in.Password
		cfg.Net = "tcp"
		cfg.Addr = net.JoinHostPort(host, port)
		cfg.DBName = database
		cfg.Timeout = 5 * time.Second
		cfg.ReadTimeout = 5 * time.Second
		cfg.WriteTimeout = 5 * time.Second
		switch sslmode {
		case "disable":
			cfg.TLSConfig = "false"
		case "require", "verify-full":
			cfg.TLSConfig = "true"
		case "prefer":
			cfg.TLSConfig = "preferred"
		}
		err = pingSQLDatabase(ctx, "mysql", cfg.FormatDSN())
	case domain.DriverMSSQL:
		if database == "" {
			writeError(w, r, errors.Join(domain.ErrInvalid,
				errors.New("database wajib diisi untuk SQL Server")))
			return
		}
		u := &url.URL{
			Scheme: "sqlserver",
			User:   url.UserPassword(in.Username, in.Password),
			Host:   net.JoinHostPort(host, port),
		}
		query := u.Query()
		query.Set("database", database)
		switch sslmode {
		case "disable":
			query.Set("encrypt", "disable")
		case "prefer":
			query.Set("encrypt", "false")
		default:
			query.Set("encrypt", "true")
		}
		if sslmode != "verify-full" {
			query.Set("trustservercertificate", "true")
		}
		u.RawQuery = query.Encode()
		err = pingSQLDatabase(ctx, "sqlserver", u.String())
	case domain.DriverOracle:
		portNumber, parseErr := strconv.Atoi(port)
		if parseErr != nil {
			writeError(w, r, errors.Join(domain.ErrInvalid,
				errors.New("port Oracle harus berupa angka")))
			return
		}
		serviceName, _ := in.Endpoint["service_name"].(string)
		sid, _ := in.Endpoint["sid"].(string)
		if serviceName == "" && sid == "" {
			serviceName = database
		}
		if serviceName == "" && sid == "" {
			writeError(w, r, errors.Join(domain.ErrInvalid,
				errors.New("service_name, sid, atau database wajib diisi untuk Oracle")))
			return
		}
		options := map[string]string{"CONNECTION TIMEOUT": "5"}
		service := serviceName
		if service == "" {
			service = sid
			options["SID"] = sid
		}
		dsn := goora.BuildUrl(host, portNumber, service, in.Username, in.Password, options)
		err = pingSQLDatabase(ctx, "oracle", dsn)
	case domain.DriverMongoDB:
		if database == "" {
			writeError(w, r, errors.Join(domain.ErrInvalid,
				errors.New("database wajib diisi untuk MongoDB")))
			return
		}
		dsn := mongoConnectionString(in.Endpoint, host, port, database, in.Username, in.Password, sslmode)
		client, connectErr := mongo.Connect(ctx, options.Client().ApplyURI(dsn))
		if connectErr != nil {
			err = connectErr
			break
		}
		defer client.Disconnect(context.Background())
		err = client.Ping(ctx, readpref.Primary())
	default:
		writeError(w, r, errors.Join(domain.ErrInvalid,
			fmt.Errorf("test connection belum didukung untuk driver %s", driver)))
		return
	}
	if err != nil {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			fmt.Errorf("database tidak terhubung atau tidak merespons: %w", err)))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "driver": in.Driver, "host": host, "database": database})
}

func mongoConnectionString(
	endpoint map[string]any,
	host, port, database, username, password, sslmode string,
) string {
	u := &url.URL{
		Scheme: "mongodb",
		Host:   net.JoinHostPort(host, port),
		Path:   "/" + database,
	}
	if username != "" {
		u.User = url.UserPassword(username, password)
	}
	query := u.Query()
	query.Set("connectTimeoutMS", "5000")
	if authSource, _ := endpoint["auth_source"].(string); strings.TrimSpace(authSource) != "" {
		query.Set("authSource", strings.TrimSpace(authSource))
	} else if username != "" {
		query.Set("authSource", "admin")
	}
	if sslmode != "disable" {
		query.Set("tls", "true")
	}
	for key, value := range endpoint {
		if !strings.HasPrefix(key, "mongo_") {
			continue
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			query.Set(strings.TrimPrefix(key, "mongo_"), strings.TrimSpace(text))
		}
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func pingSQLDatabase(ctx context.Context, driver, dsn string) error {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	return db.PingContext(ctx)
}

// --- connector ---

// handleListConnectors mengembalikan daftar konektor yang bisa dipilih sebagai
// `extractor`/`loader` pipeline.
//
// Tanpa tenant_id, hasilnya katalog statis (source=catalog) - cukup untuk
// mengisi dropdown, tetapi belum tahu plugin apa yang terpasang di tenant.
// Dengan tenant_id, control plane menjalankan `meltano plugin list --format=json` di
// project tenant dan menandai setiap konektor dengan installed=true/false
// (source=tenant).
func (s *Server) handleListConnectors(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	kind := domain.PluginKind(strings.TrimSpace(query.Get("kind")))
	if kind != "" && !kind.Valid() {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("kind harus extractor atau loader")))
		return
	}
	driver := domain.Driver(strings.TrimSpace(query.Get("driver")))
	if driver != "" && !driver.Valid() {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("driver tidak dikenal: "+string(driver))))
		return
	}

	items := connector.Catalog(kind, driver)
	resp := map[string]any{
		"source":   "catalog",
		"data":     items,
		"has_more": false,
	}

	tenantID := strings.TrimSpace(query.Get("tenant_id"))
	if tenantID == "" {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if s.connectors == nil {
		writeError(w, r, errors.New("mode tenant GET /v1/connectors tidak dikonfigurasi"))
		return
	}

	projectDir, err := s.tenantProjectDir(r.Context(), tenantID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	plugins, err := s.connectors.ListPlugins(r.Context(), projectDir)
	if err != nil {
		writeError(w, r, err)
		return
	}

	resp["source"] = "tenant"
	resp["tenant_id"] = tenantID
	resp["data"] = mergeInstalledConnectors(items, plugins, kind, driver)
	writeJSON(w, http.StatusOK, resp)
}

// tenantProjectDir mengembalikan direktori project Meltano milik tenant.
// ProjectDir dari store menang; CP_TENANTS_ROOT/<nama tenant> hanya fallback.
func (s *Server) tenantProjectDir(ctx context.Context, tenantID string) (string, error) {
	tenant, err := s.store.GetTenant(ctx, tenantID)
	if err != nil {
		return "", err
	}
	if dir := strings.TrimSpace(tenant.ProjectDir); dir != "" {
		return dir, nil
	}
	if s.tenantsRoot == "" {
		return "", errors.Join(domain.ErrInvalid,
			errors.New("tenant tidak punya project_dir dan CP_TENANTS_ROOT kosong"))
	}
	return filepath.Join(s.tenantsRoot, tenant.Name), nil
}

// mergeInstalledConnectors menandai katalog dengan plugin yang benar-benar ada
// di project tenant, lalu menambahkan plugin tenant yang tidak ada di katalog
// (mis. connector non-database atau plugin custom).
func mergeInstalledConnectors(
	catalog []domain.Connector,
	plugins []meltano.Plugin,
	kind domain.PluginKind,
	driver domain.Driver,
) []domain.Connector {
	installed := true
	index := make(map[string]int, len(catalog))
	out := make([]domain.Connector, len(catalog))
	copy(out, catalog)
	for i, c := range out {
		index[c.Name] = i
	}

	for _, p := range plugins {
		pluginKind := domain.PluginKind(p.Type)
		if !pluginKind.Valid() {
			continue
		}
		if i, ok := index[p.Name]; ok {
			out[i].Installed = &installed
			out[i].Variant = p.Variant
			out[i].Custom = p.Custom
			continue
		}
		pluginDriver, _ := connector.DriverForPlugin(p.Name)
		if kind != "" && pluginKind != kind {
			continue
		}
		if driver != "" && pluginDriver != driver {
			continue
		}
		label, ok := connector.LabelForPlugin(p.Name)
		if !ok {
			label = p.Name
		}
		out = append(out, domain.Connector{
			Name:      p.Name,
			Kind:      pluginKind,
			Driver:    pluginDriver,
			Label:     label,
			Installed: &installed,
			Variant:   p.Variant,
			Custom:    p.Custom,
		})
	}
	return out
}

// --- pipeline ---

func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListPipelines(r.Context(), r.URL.Query().Get("tenant_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "has_more": false})
}

func (s *Server) handleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TenantID                string                         `json:"tenant_id"`
		Name                    string                         `json:"name"`
		Extractor               string                         `json:"extractor"`
		Loader                  string                         `json:"loader"`
		SourceConnectionID      string                         `json:"source_connection_id"`
		DestinationConnectionID string                         `json:"destination_connection_id"`
		StateIDSuffix           *string                        `json:"state_id_suffix"`
		DefaultTimeoutSeconds   int                            `json:"default_timeout_seconds"`
		MaxAttempts             int                            `json:"max_attempts"`
		Enabled                 *bool                          `json:"enabled"`
		SourceFilter            domain.PipelineSourceFilter    `json:"source_filter"`
		TransferOptions         domain.PipelineTransferOptions `json:"transfer_options"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.TenantID) == "" ||
		strings.TrimSpace(in.Name) == "" ||
		strings.TrimSpace(in.Extractor) == "" ||
		strings.TrimSpace(in.Loader) == "" ||
		strings.TrimSpace(in.SourceConnectionID) == "" ||
		strings.TrimSpace(in.DestinationConnectionID) == "" {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("tenant_id, name, extractor, loader, source_connection_id, dan destination_connection_id wajib diisi")))
		return
	}
	if _, err := s.validatePipelineConnections(
		r.Context(),
		in.TenantID,
		in.Extractor,
		in.Loader,
		in.SourceConnectionID,
		in.DestinationConnectionID,
	); err != nil {
		writeError(w, r, err)
		return
	}
	sourceFilter, err := normalizePipelineSourceFilter(in.SourceFilter, in.Extractor)
	if err != nil {
		writeError(w, r, err)
		return
	}
	transferOptions, err := normalizePipelineTransferOptions(in.TransferOptions, in.Loader)
	if err != nil {
		writeError(w, r, err)
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	p, err := s.store.CreatePipeline(r.Context(), domain.Pipeline{
		TenantID:                in.TenantID,
		Name:                    in.Name,
		Extractor:               in.Extractor,
		Loader:                  in.Loader,
		SourceConnectionID:      in.SourceConnectionID,
		DestinationConnectionID: in.DestinationConnectionID,
		StateIDSuffix:           in.StateIDSuffix,
		DefaultTimeoutSeconds:   in.DefaultTimeoutSeconds,
		MaxAttempts:             in.MaxAttempts,
		Enabled:                 enabled,
		SourceFilter:            sourceFilter,
		TransferOptions:         transferOptions,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPipeline(r.Context(), r.PathValue("pipelineID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdatePipeline(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name                    *string                         `json:"name"`
		Extractor               *string                         `json:"extractor"`
		Loader                  *string                         `json:"loader"`
		SourceConnectionID      *string                         `json:"source_connection_id"`
		DestinationConnectionID *string                         `json:"destination_connection_id"`
		StateIDSuffix           *string                         `json:"state_id_suffix"`
		DefaultTimeoutSeconds   *int                            `json:"default_timeout_seconds"`
		MaxAttempts             *int                            `json:"max_attempts"`
		Enabled                 *bool                           `json:"enabled"`
		SourceFilter            *domain.PipelineSourceFilter    `json:"source_filter"`
		TransferOptions         *domain.PipelineTransferOptions `json:"transfer_options"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	p, err := s.store.GetPipeline(r.Context(), r.PathValue("pipelineID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.Extractor != nil {
		p.Extractor = strings.TrimSpace(*in.Extractor)
	}
	if in.Loader != nil {
		p.Loader = strings.TrimSpace(*in.Loader)
	}
	if in.SourceConnectionID != nil {
		p.SourceConnectionID = strings.TrimSpace(*in.SourceConnectionID)
	}
	if in.DestinationConnectionID != nil {
		p.DestinationConnectionID = strings.TrimSpace(*in.DestinationConnectionID)
	}
	if in.StateIDSuffix != nil {
		suffix := strings.TrimSpace(*in.StateIDSuffix)
		if suffix == "" {
			p.StateIDSuffix = nil
		} else {
			p.StateIDSuffix = &suffix
		}
	}
	if in.DefaultTimeoutSeconds != nil {
		p.DefaultTimeoutSeconds = *in.DefaultTimeoutSeconds
	}
	if in.MaxAttempts != nil {
		p.MaxAttempts = *in.MaxAttempts
	}
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	if in.SourceFilter != nil {
		sourceFilter, filterErr := normalizePipelineSourceFilter(*in.SourceFilter, p.Extractor)
		if filterErr != nil {
			writeError(w, r, filterErr)
			return
		}
		p.SourceFilter = sourceFilter
	}
	if in.TransferOptions != nil {
		transferOptions, transferErr := normalizePipelineTransferOptions(*in.TransferOptions, p.Loader)
		if transferErr != nil {
			writeError(w, r, transferErr)
			return
		}
		p.TransferOptions = transferOptions
	}
	if p.Name == "" || p.Extractor == "" || p.Loader == "" ||
		p.SourceConnectionID == "" || p.DestinationConnectionID == "" {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("name, extractor, loader, source, dan destination wajib diisi")))
		return
	}
	if p.DefaultTimeoutSeconds < 1 || p.MaxAttempts < 1 {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("default_timeout_seconds dan max_attempts minimal 1")))
		return
	}
	if _, err := s.validatePipelineConnections(
		r.Context(),
		p.TenantID,
		p.Extractor,
		p.Loader,
		p.SourceConnectionID,
		p.DestinationConnectionID,
	); err != nil {
		writeError(w, r, err)
		return
	}
	normalizedFilter, err := normalizePipelineSourceFilter(p.SourceFilter, p.Extractor)
	if err != nil {
		writeError(w, r, err)
		return
	}
	p.SourceFilter = normalizedFilter
	p, err = s.store.UpdatePipeline(r.Context(), p)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePipeline(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeletePipeline(r.Context(), r.PathValue("pipelineID")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleValidatePipeline menghitung state_id dan memeriksa prasyarat tanpa
// menjalankan apa pun. Pemetaan penuhnya ke `meltano run --dry-run` belum
// diimplementasikan (lihat README).
func (s *Server) handleValidatePipeline(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPipeline(r.Context(), r.PathValue("pipelineID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	t, err := s.store.GetTenant(r.Context(), p.TenantID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	suffix := ""
	if p.StateIDSuffix != nil {
		suffix = *p.StateIDSuffix
	}
	stateID, err := meltano.StateID(t.EnvironmentName, p.Extractor, p.Loader, suffix)
	checks := []map[string]any{
		{"name": "extractor_present", "ok": p.Extractor != ""},
		{"name": "loader_present", "ok": p.Loader != ""},
		{"name": "state_id_suffix_charset", "ok": err == nil},
		{"name": "environment_present", "ok": t.EnvironmentName != ""},
	}
	connectionErr := error(nil)
	if err == nil {
		_, connectionErr = s.validatePipelineConnections(
			r.Context(),
			p.TenantID,
			p.Extractor,
			p.Loader,
			p.SourceConnectionID,
			p.DestinationConnectionID,
		)
	}
	checks = append(checks, map[string]any{
		"name": "connections_match_pipeline",
		"ok":   connectionErr == nil,
	})
	valid := err == nil && connectionErr == nil
	resp := map[string]any{"valid": valid, "checks": checks}
	if valid {
		resp["resolved_state_id"] = stateID
	}
	status := http.StatusOK
	if !valid {
		status = http.StatusUnprocessableEntity
		resp["valid"] = false
		switch {
		case err != nil:
			resp["detail"] = err.Error()
		case connectionErr != nil:
			resp["detail"] = connectionErr.Error()
		}
	}
	writeJSON(w, status, resp)
}

// validatePipelineConnections memastikan A dan B yang dipilih benar-benar
// sumber/tujuan milik tenant yang sama dan cocok dengan plugin pipeline.
func normalizePipelineSourceFilter(
	in domain.PipelineSourceFilter,
	extractor string,
) (domain.PipelineSourceFilter, error) {
	out := domain.PipelineSourceFilter{
		Streams:   make([]string, 0, len(in.Streams)),
		RowFilter: strings.TrimSpace(in.RowFilter),
	}
	if len(in.Streams) > 100 {
		return domain.PipelineSourceFilter{}, errors.Join(domain.ErrInvalid,
			errors.New("source_filter.streams maksimal 100 pola"))
	}
	seen := make(map[string]bool, len(in.Streams))
	for _, raw := range in.Streams {
		stream := strings.TrimSpace(raw)
		if stream == "" || seen[stream] {
			continue
		}
		if len(stream) > 255 || strings.ContainsAny(stream, "\r\n\t") {
			return domain.PipelineSourceFilter{}, errors.Join(domain.ErrInvalid,
				fmt.Errorf("pola stream %q tidak valid", stream))
		}
		seen[stream] = true
		out.Streams = append(out.Streams, stream)
	}
	if len(out.RowFilter) > 1000 || strings.ContainsAny(out.RowFilter, "\r\n;") {
		return domain.PipelineSourceFilter{}, errors.Join(domain.ErrInvalid,
			errors.New("source_filter.row_filter maksimal 1000 karakter dan tidak boleh berisi newline atau ';'"))
	}
	if out.RowFilter != "" && !extractorSupportsRowFilter(extractor) {
		return domain.PipelineSourceFilter{}, errors.Join(domain.ErrInvalid,
			fmt.Errorf("extractor %s belum mendukung source_filter.row_filter; gunakan pemilihan stream saja", extractor))
	}
	return out, nil
}

func extractorSupportsRowFilter(extractor string) bool {
	base, _, _ := strings.Cut(extractor, "--")
	switch base {
	case "tap-postgres", "tap-mysql", "tap-mssql", "tap-mongodb",
		"tap-rest-api-msdk", "tap-csv", "tap-google-sheets":
		return true
	default:
		return false
	}
}

func normalizePipelineTransferOptions(
	in domain.PipelineTransferOptions,
	loader string,
) (domain.PipelineTransferOptions, error) {
	out := domain.PipelineTransferOptions{
		BatchSizeRows:   in.BatchSizeRows,
		ValidateRecords: in.ValidateRecords,
		LoadMethod:      strings.ToLower(strings.TrimSpace(in.LoadMethod)),
	}
	if out.BatchSizeRows == 0 && out.ValidateRecords == nil && out.LoadMethod == "" {
		return out, nil
	}
	base, _, _ := strings.Cut(loader, "--")
	if base != "target-postgres" {
		return domain.PipelineTransferOptions{}, errors.Join(domain.ErrInvalid,
			fmt.Errorf("transfer_options hanya didukung target-postgres, bukan %s", loader))
	}
	if out.BatchSizeRows != 0 && (out.BatchSizeRows < 1000 || out.BatchSizeRows > 1000000) {
		return domain.PipelineTransferOptions{}, errors.Join(domain.ErrInvalid,
			errors.New("transfer_options.batch_size_rows harus 1000..1000000"))
	}
	if out.LoadMethod != "" &&
		out.LoadMethod != "upsert" &&
		out.LoadMethod != "overwrite" &&
		out.LoadMethod != "append-only" {
		return domain.PipelineTransferOptions{}, errors.Join(domain.ErrInvalid,
			errors.New("transfer_options.load_method harus upsert, overwrite, atau append-only"))
	}
	return out, nil
}

func (s *Server) validatePipelineConnections(
	ctx context.Context,
	tenantID, extractor, loader, sourceID, destinationID string,
) ([]domain.Connection, error) {
	if _, err := s.store.GetTenant(ctx, tenantID); err != nil {
		return nil, err
	}
	source, err := s.store.GetConnection(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	destination, err := s.store.GetConnection(ctx, destinationID)
	if err != nil {
		return nil, err
	}
	if source.TenantID != tenantID {
		return nil, errors.Join(domain.ErrInvalid,
			errors.New("source_connection_id bukan milik tenant pipeline"))
	}
	if destination.TenantID != tenantID {
		return nil, errors.Join(domain.ErrInvalid,
			errors.New("destination_connection_id bukan milik tenant pipeline"))
	}
	if source.Kind != domain.KindSource {
		return nil, errors.Join(domain.ErrInvalid,
			errors.New("source_connection_id harus menunjuk koneksi kind=source"))
	}
	if destination.Kind != domain.KindDestination {
		return nil, errors.Join(domain.ErrInvalid,
			errors.New("destination_connection_id harus menunjuk koneksi kind=destination"))
	}
	if driver, ok := connector.DriverForPlugin(extractor); ok && driver != source.Driver {
		return nil, errors.Join(domain.ErrInvalid,
			fmt.Errorf("extractor %s membutuhkan source driver %s, bukan %s",
				extractor, driver, source.Driver))
	}
	if driver, ok := connector.DriverForPlugin(loader); ok && driver != destination.Driver {
		return nil, errors.Join(domain.ErrInvalid,
			fmt.Errorf("loader %s membutuhkan destination driver %s, bukan %s",
				loader, driver, destination.Driver))
	}
	return []domain.Connection{source, destination}, nil
}

// --- run ---

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	pipelineID := r.PathValue("pipelineID")
	p, err := s.store.GetPipeline(r.Context(), pipelineID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("header Idempotency-Key wajib diisi")))
		return
	}

	var in struct {
		FullRefresh    bool    `json:"full_refresh"`
		RefreshCatalog bool    `json:"refresh_catalog"`
		StateIDSuffix  *string `json:"state_id_suffix"`
		TimeoutSeconds int     `json:"timeout_seconds"`
		Trigger        string  `json:"trigger"`
		Reason         string  `json:"reason"`
	}
	if r.ContentLength > 0 && !decodeJSON(w, r, &in) {
		return
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = p.DefaultTimeoutSeconds
	}
	if in.Trigger == "" {
		in.Trigger = "manual"
	}
	// Field `force` sengaja tidak didukung: menonaktifkan satu-satunya pengaman
	// yang menggantikan advisory check Meltano (doc 14 bagian 14.8).
	suffix := p.StateIDSuffix
	if in.StateIDSuffix != nil {
		suffix = in.StateIDSuffix
	}
	run, err := s.store.CreateRun(r.Context(), domain.Run{
		PipelineID:     pipelineID,
		Status:         domain.StatusQueued,
		Attempt:        1,
		Trigger:        in.Trigger,
		Reason:         in.Reason,
		TimeoutSeconds: in.TimeoutSeconds,
		FullRefresh:    in.FullRefresh,
		StateIDSuffix:  suffix,
	}, idempotencyKey)
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/runs/"+run.ID)
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	var statuses []domain.RunStatus
	for _, raw := range r.URL.Query()["status"] {
		st := domain.RunStatus(raw)
		if !st.Valid() {
			writeError(w, r, errors.Join(domain.ErrInvalid, errors.New("status tidak dikenal: "+raw)))
			return
		}
		statuses = append(statuses, st)
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			writeError(w, r, errors.Join(domain.ErrInvalid, errors.New("limit harus 1..200")))
			return
		}
		limit = n
	}
	items, err := s.store.ListRuns(r.Context(), store.RunFilter{
		PipelineID: r.URL.Query().Get("pipeline_id"),
		Statuses:   statuses,
		Limit:      limit,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "has_more": false})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetRun(r.Context(), r.PathValue("runID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleGetRunLogs(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 500 {
			writeError(w, r, errors.Join(domain.ErrInvalid, errors.New("limit harus 1..500")))
			return
		}
		limit = n
	}

	run, err := s.store.GetRun(r.Context(), r.PathValue("runID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	pipeline, err := s.store.GetPipeline(r.Context(), run.PipelineID)
	if err != nil {
		writeError(w, r, err)
		return
	}

	lines := make([]meltano.ExecutionLogLine, 0)
	if run.Status.IsTerminal() && strings.TrimSpace(run.LogTail) != "" {
		for _, raw := range strings.Split(run.LogTail, "\n") {
			if strings.TrimSpace(raw) != "" {
				lines = append(lines, meltano.ParseExecutionLine(raw))
			}
		}
	} else {
		events, eventErr := s.store.ListEvents(r.Context(), run.ID, 500)
		if eventErr != nil {
			writeError(w, r, eventErr)
			return
		}
		for _, event := range events {
			if event.Kind != "log" {
				continue
			}
			raw, _ := event.Payload["raw"].(string)
			if strings.TrimSpace(raw) == "" {
				continue
			}
			line := meltano.ParseExecutionLine(raw)
			if line.At == nil && !event.At.IsZero() {
				at := event.At
				line.At = &at
			}
			lines = append(lines, line)
		}
	}

	summary := meltano.SummarizeExecutionLog(lines, pipeline.Extractor, pipeline.Loader, run)
	truncated := len(lines) > limit
	if truncated {
		lines = lines[len(lines)-limit:]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"run":     run,
		"summary": summary,
		"log": map[string]any{
			"line_count": len(lines),
			"truncated":  truncated,
			"lines":      lines,
		},
	})
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.RequestCancel(r.Context(), r.PathValue("runID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	now := time.Now().UTC()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":              run.Status,
		"cancel_requested_at": now,
	})
}

func (s *Server) handleRetryRun(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("header Idempotency-Key wajib diisi")))
		return
	}
	prev, err := s.store.GetRun(r.Context(), r.PathValue("runID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !prev.Status.IsTerminal() {
		writeError(w, r, errors.Join(domain.ErrConflict,
			errors.New("run belum selesai; batalkan dulu sebelum retry")))
		return
	}
	id := prev.ID
	run, err := s.store.CreateRun(r.Context(), domain.Run{
		PipelineID:     prev.PipelineID,
		Status:         domain.StatusQueued,
		Attempt:        1,
		RetriedFrom:    &id,
		Trigger:        "retry",
		TimeoutSeconds: prev.TimeoutSeconds,
		FullRefresh:    prev.FullRefresh,
		StateIDSuffix:  prev.StateIDSuffix,
	}, idempotencyKey)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.ListEvents(r.Context(), r.PathValue("runID"), 200)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": events, "has_more": false})
}
