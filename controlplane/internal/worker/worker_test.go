package worker

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/example/controlplane/internal/domain"
	"github.com/example/controlplane/internal/store/memory"
)

func TestConventionEnvBuilderPostgresSource(t *testing.T) {
	conn := domain.Connection{
		Driver:   domain.DriverPostgres,
		Username: "reader@example.com",
		Endpoint: map[string]any{
			"host":     "source.internal",
			"port":     "5433",
			"database": "orders",
			"schema":   "sales",
			"sslmode":  "require",
		},
	}

	env, err := (ConventionEnvBuilder{}).Build(
		domain.KindSource,
		conn,
		"p@ss:word/with?special&chars",
	)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	raw := env["TAP_POSTGRES_SQLALCHEMY_URL"]
	if raw == "" {
		t.Fatal("TAP_POSTGRES_SQLALCHEMY_URL tidak diisi")
	}
	if env["MELTANO_EXTRACT_SQLALCHEMY_URL"] != raw {
		t.Fatalf("URL generik tidak sama dengan URL plugin: %q != %q",
			env["MELTANO_EXTRACT_SQLALCHEMY_URL"], raw)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("URL tidak valid: %v", err)
	}
	if u.Scheme != "postgresql" {
		t.Fatalf("scheme = %q, ingin postgresql", u.Scheme)
	}
	if u.Host != "source.internal:5433" {
		t.Fatalf("host = %q", u.Host)
	}
	if u.Path != "/orders" {
		t.Fatalf("path = %q", u.Path)
	}
	if got := u.Query().Get("sslmode"); got != "require" {
		t.Fatalf("sslmode = %q", got)
	}
	if got, _ := u.User.Password(); got != "p@ss:word/with?special&chars" {
		t.Fatalf("password tidak ter-escape/ter-decode dengan benar: %q", got)
	}
	if env["MELTANO_EXTRACT__LOAD_SCHEMA"] != "sales" {
		t.Fatalf("load schema = %q", env["MELTANO_EXTRACT__LOAD_SCHEMA"])
	}

	var schemas []string
	if err := json.Unmarshal([]byte(env["TAP_POSTGRES_FILTER_SCHEMAS"]), &schemas); err != nil {
		t.Fatalf("filter schemas bukan JSON valid: %v", err)
	}
	if len(schemas) != 1 || schemas[0] != "sales" {
		t.Fatalf("filter schemas = %#v", schemas)
	}
}

func TestConventionEnvBuilderPostgresDestination(t *testing.T) {
	conn := domain.Connection{
		Driver:   domain.DriverPostgres,
		Username: "writer",
		Endpoint: map[string]any{
			"host":     "target.internal",
			"port":     "5432",
			"database": "warehouse",
			"schema":   "analytics",
		},
		Options: map[string]any{
			"load_method":      "upsert",
			"batch_size_rows":  float64(50000),
			"validate_records": false,
		},
	}

	env, err := (ConventionEnvBuilder{}).Build(
		domain.KindDestination,
		conn,
		"secret",
	)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	raw := env["TARGET_POSTGRES_SQLALCHEMY_URL"]
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("URL tidak valid: %v", err)
	}
	if u.Scheme != "postgresql+psycopg" {
		t.Fatalf("scheme = %q, ingin postgresql+psycopg", u.Scheme)
	}
	if env["TARGET_POSTGRES_DEFAULT_TARGET_SCHEMA"] != "analytics" {
		t.Fatalf("default target schema = %q",
			env["TARGET_POSTGRES_DEFAULT_TARGET_SCHEMA"])
	}
	if env["MELTANO_EXTRACT__LOAD_SCHEMA"] != "analytics" {
		t.Fatalf("load schema = %q", env["MELTANO_EXTRACT__LOAD_SCHEMA"])
	}
	if env["TARGET_POSTGRES_LOAD_METHOD"] != "upsert" {
		t.Fatalf("load method = %q", env["TARGET_POSTGRES_LOAD_METHOD"])
	}
	if env["TARGET_POSTGRES_BATCH_SIZE_ROWS"] != "50000" {
		t.Fatalf("batch size = %q", env["TARGET_POSTGRES_BATCH_SIZE_ROWS"])
	}
	if env["TARGET_POSTGRES_VALIDATE_RECORDS"] != "false" {
		t.Fatalf("validate records = %q", env["TARGET_POSTGRES_VALIDATE_RECORDS"])
	}
}

func TestConventionEnvBuilderPostgresDestinationDefaultsToUpsert(t *testing.T) {
	env, err := (ConventionEnvBuilder{}).Build(
		domain.KindDestination,
		domain.Connection{
			Driver:   domain.DriverPostgres,
			Username: "writer",
			Endpoint: map[string]any{
				"host":     "target.internal",
				"database": "warehouse",
			},
		},
		"secret",
	)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if env["TARGET_POSTGRES_LOAD_METHOD"] != "upsert" {
		t.Fatalf("load method default = %q, ingin upsert",
			env["TARGET_POSTGRES_LOAD_METHOD"])
	}
}

func TestConventionEnvBuilderAcceptsNumericJSONPort(t *testing.T) {
	env, err := (ConventionEnvBuilder{}).Build(
		domain.KindSource,
		domain.Connection{
			Driver:   domain.DriverPostgres,
			Username: "reader",
			Endpoint: map[string]any{
				"host":     "source.internal",
				"port":     float64(55441),
				"database": "app",
			},
		},
		"secret",
	)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	u, err := url.Parse(env["TAP_POSTGRES_SQLALCHEMY_URL"])
	if err != nil {
		t.Fatalf("URL tidak valid: %v", err)
	}
	if u.Host != "source.internal:55441" {
		t.Fatalf("host = %q, ingin source.internal:55441", u.Host)
	}
}

func TestConventionEnvBuilderPostgresRequiresCredentials(t *testing.T) {
	_, err := (ConventionEnvBuilder{}).Build(
		domain.KindSource,
		domain.Connection{
			Driver:   domain.DriverPostgres,
			Username: "reader",
			Endpoint: map[string]any{
				"host":     "source.internal",
				"database": "orders",
			},
		},
		"",
	)
	if err == nil {
		t.Fatal("password kosong harus ditolak")
	}
}

func TestConventionEnvBuilderMySQL(t *testing.T) {
	conn := domain.Connection{
		Driver:   domain.DriverMySQL,
		Username: "reader",
		Endpoint: map[string]any{
			"host":     "mysql.internal",
			"port":     "3306",
			"database": "orders",
		},
	}
	sourceEnv, err := (ConventionEnvBuilder{}).Build(domain.KindSource, conn, "secret")
	if err != nil {
		t.Fatalf("source Build: %v", err)
	}
	u, err := url.Parse(sourceEnv["TAP_MYSQL_SQLALCHEMY_URL"])
	if err != nil {
		t.Fatalf("URL MySQL tidak valid: %v", err)
	}
	if u.Scheme != "mysql+pymysql" || u.Path != "/orders" {
		t.Fatalf("URL MySQL = %q", u.String())
	}
	if sourceEnv["TAP_MYSQL_USER"] != "reader" {
		t.Fatalf("tap user = %q", sourceEnv["TAP_MYSQL_USER"])
	}

	conn.Kind = domain.KindDestination
	destinationEnv, err := (ConventionEnvBuilder{}).Build(
		domain.KindDestination, conn, "secret",
	)
	if err != nil {
		t.Fatalf("destination Build: %v", err)
	}
	if destinationEnv["TARGET_MYSQL_USERNAME"] != "reader" {
		t.Fatalf("target username = %q", destinationEnv["TARGET_MYSQL_USERNAME"])
	}
	if destinationEnv["TARGET_MYSQL_DEFAULT_TARGET_SCHEMA"] != "orders" {
		t.Fatalf("target schema = %q",
			destinationEnv["TARGET_MYSQL_DEFAULT_TARGET_SCHEMA"])
	}
}

func TestConventionEnvBuilderMSSQL(t *testing.T) {
	conn := domain.Connection{
		Driver:   domain.DriverMSSQL,
		Username: "reader",
		Endpoint: map[string]any{
			"host":     "mssql.internal",
			"port":     float64(1433),
			"database": "orders",
			"schema":   "dbo",
		},
	}
	sourceEnv, err := (ConventionEnvBuilder{}).Build(domain.KindSource, conn, "secret")
	if err != nil {
		t.Fatalf("source Build: %v", err)
	}
	if sourceEnv["TAP_MSSQL_DIALECT"] != "mssql" ||
		sourceEnv["TAP_MSSQL_DRIVER_TYPE"] != "pymssql" {
		t.Fatalf("MSSQL source mapping tidak lengkap: %#v", sourceEnv)
	}

	destinationEnv, err := (ConventionEnvBuilder{}).Build(
		domain.KindDestination, conn, "secret",
	)
	if err != nil {
		t.Fatalf("destination Build: %v", err)
	}
	if destinationEnv["TARGET_MSSQL_USERNAME"] != "reader" ||
		destinationEnv["TARGET_MSSQL_DEFAULT_TARGET_SCHEMA"] != "dbo" {
		t.Fatalf("MSSQL target mapping tidak lengkap: %#v", destinationEnv)
	}
}

func TestConventionEnvBuilderMongoDB(t *testing.T) {
	conn := domain.Connection{
		Driver:   domain.DriverMongoDB,
		Username: "mongo_user",
		Endpoint: map[string]any{
			"host":        "mongo.internal",
			"port":        "27017",
			"database":    "app",
			"auth_source": "admin",
			"sslmode":     "require",
		},
	}
	sourceEnv, err := (ConventionEnvBuilder{}).Build(domain.KindSource, conn, "secret")
	if err != nil {
		t.Fatalf("source Build: %v", err)
	}
	raw := sourceEnv["TAP_MONGODB_MONGODB_CONNECTION_STRING"]
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("Mongo URI tidak valid: %v", err)
	}
	if u.Scheme != "mongodb" || u.Path != "/app" ||
		u.Query().Get("authSource") != "admin" || u.Query().Get("tls") != "true" {
		t.Fatalf("Mongo source URI = %s", raw)
	}
	if sourceEnv["TAP_MONGODB_DATABASE"] != "app" {
		t.Fatalf("Mongo database = %q", sourceEnv["TAP_MONGODB_DATABASE"])
	}

	conn.Kind = domain.KindDestination
	destinationEnv, err := (ConventionEnvBuilder{}).Build(domain.KindDestination, conn, "secret")
	if err != nil {
		t.Fatalf("destination Build: %v", err)
	}
	if destinationEnv["TARGET_MONGODB_CONNECTION_STRING"] == "" ||
		destinationEnv["TARGET_MONGODB_DB_NAME"] != "app" {
		t.Fatalf("Mongo target env = %#v", destinationEnv)
	}
}

func TestHeartbeatCancelsRunningMeltano(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	tenant, err := st.CreateTenant(ctx, domain.Tenant{Name: "cancel-test", EnvironmentName: "dev"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	pipeline, err := st.CreatePipeline(ctx, domain.Pipeline{
		TenantID:              tenant.ID,
		Name:                  "cancel-test",
		Extractor:             "tap-postgres",
		Loader:                "target-postgres",
		DefaultTimeoutSeconds: 60,
		MaxAttempts:           1,
		Enabled:               true,
	})
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	run, err := st.CreateRun(ctx, domain.Run{
		PipelineID:     pipeline.ID,
		Status:         domain.StatusQueued,
		Trigger:        "manual",
		TimeoutSeconds: 60,
	}, "cancel-heartbeat-test")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	claimed, err := st.ClaimRun(ctx, "worker-test", time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("ClaimRun: %v, %#v", err, claimed)
	}
	if _, err := st.RequestCancel(ctx, run.ID); err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}

	w := &Worker{
		Store: st,
		Cfg: Config{
			WorkerID:  "worker-test",
			Lease:     time.Minute,
			Heartbeat: 10 * time.Millisecond,
		},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	cancelled := make(chan struct{})
	heartbeatErr := w.startHeartbeat(heartbeatCtx, run.ID, func() {
		close(cancelled)
	})

	select {
	case <-cancelled:
	case err := <-heartbeatErr:
		t.Fatalf("heartbeat berhenti sebelum cancel: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat tidak meneruskan permintaan cancel")
	}
}

func TestConventionEnvBuilderOracle(t *testing.T) {
	conn := domain.Connection{
		Driver:   domain.DriverOracle,
		Username: "reader",
		Endpoint: map[string]any{
			"host":         "oracle.internal",
			"port":         "1521",
			"database":     "FINANCE",
			"service_name": "FINANCE",
			"schema":       "REPORTING",
		},
	}
	sourceEnv, err := (ConventionEnvBuilder{}).Build(domain.KindSource, conn, "secret")
	if err != nil {
		t.Fatalf("source Build: %v", err)
	}
	if sourceEnv["TAP_ORACLE_SERVICE_NAME"] != "FINANCE" ||
		sourceEnv["TAP_ORACLE_USER"] != "reader" ||
		sourceEnv["TAP_ORACLE_FILTER_SCHEMAS"] != "REPORTING" {
		t.Fatalf("Oracle source mapping tidak lengkap: %#v", sourceEnv)
	}

	destinationEnv, err := (ConventionEnvBuilder{}).Build(
		domain.KindDestination, conn, "secret",
	)
	if err != nil {
		t.Fatalf("destination Build: %v", err)
	}
	if destinationEnv["TARGET_ORACLE_USERNAME"] != "reader" ||
		destinationEnv["TARGET_ORACLE_DRIVER_NAME"] != "oracle+cx_oracle" {
		t.Fatalf("Oracle target mapping tidak lengkap: %#v", destinationEnv)
	}
}
