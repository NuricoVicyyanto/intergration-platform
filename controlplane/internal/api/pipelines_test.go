package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/controlplane/internal/domain"
	"github.com/example/controlplane/internal/store/memory"
)

func createTenant(t *testing.T, st *memory.Store, name string) domain.Tenant {
	t.Helper()
	tenant, err := st.CreateTenant(context.Background(), domain.Tenant{
		Name: name, EnvironmentName: "dev", ProjectDir: "/tenants/" + name,
	})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	return tenant
}

func createConnection(
	t *testing.T,
	st *memory.Store,
	tenantID, name string,
	kind domain.ConnectionKind,
	driver domain.Driver,
) domain.Connection {
	t.Helper()
	conn, err := st.CreateConnection(context.Background(), domain.Connection{
		TenantID: tenantID,
		Name:     name,
		Kind:     kind,
		Driver:   driver,
		Endpoint: map[string]any{
			"host":     "db.internal",
			"database": "app",
		},
		Username: "reader",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}
	return conn
}

func doJSON(
	t *testing.T,
	srv *Server,
	method, target string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestCreatePipelineAcceptsMatchingConnections(t *testing.T) {
	srv, st := newTestServer(nil, "")
	tenant := createTenant(t, st, "acme")
	source := createConnection(
		t, st, tenant.ID, "source", domain.KindSource, domain.DriverPostgres,
	)
	destination := createConnection(
		t, st, tenant.ID, "destination", domain.KindDestination, domain.DriverPostgres,
	)

	rec := doJSON(t, srv, http.MethodPost, "/v1/pipelines", map[string]any{
		"tenant_id":                 tenant.ID,
		"name":                      "a-to-b",
		"extractor":                 "tap-postgres",
		"loader":                    "target-postgres",
		"source_connection_id":      source.ID,
		"destination_connection_id": destination.ID,
		"default_timeout_seconds":   60,
		"max_attempts":              1,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, ingin 201 (%s)", rec.Code, rec.Body.String())
	}
}

func TestCreatePipelineRejectsWrongConnectionKind(t *testing.T) {
	srv, st := newTestServer(nil, "")
	tenant := createTenant(t, st, "acme")
	source := createConnection(
		t, st, tenant.ID, "source", domain.KindSource, domain.DriverPostgres,
	)
	destination := createConnection(
		t, st, tenant.ID, "also-source", domain.KindSource, domain.DriverPostgres,
	)

	rec := doJSON(t, srv, http.MethodPost, "/v1/pipelines", map[string]any{
		"tenant_id":                 tenant.ID,
		"name":                      "a-to-b",
		"extractor":                 "tap-postgres",
		"loader":                    "target-postgres",
		"source_connection_id":      source.ID,
		"destination_connection_id": destination.ID,
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, ingin 422 (%s)", rec.Code, rec.Body.String())
	}
}

func TestCreatePipelineRejectsCrossTenantConnections(t *testing.T) {
	srv, st := newTestServer(nil, "")
	tenant := createTenant(t, st, "acme")
	other := createTenant(t, st, "other")
	source := createConnection(
		t, st, other.ID, "source", domain.KindSource, domain.DriverPostgres,
	)
	destination := createConnection(
		t, st, tenant.ID, "destination", domain.KindDestination, domain.DriverPostgres,
	)

	rec := doJSON(t, srv, http.MethodPost, "/v1/pipelines", map[string]any{
		"tenant_id":                 tenant.ID,
		"name":                      "a-to-b",
		"extractor":                 "tap-postgres",
		"loader":                    "target-postgres",
		"source_connection_id":      source.ID,
		"destination_connection_id": destination.ID,
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, ingin 422 (%s)", rec.Code, rec.Body.String())
	}
}

func TestCreateRunRequiresIdempotencyKey(t *testing.T) {
	srv, st := newTestServer(nil, "")
	tenant := createTenant(t, st, "acme")
	source := createConnection(
		t, st, tenant.ID, "source", domain.KindSource, domain.DriverPostgres,
	)
	destination := createConnection(
		t, st, tenant.ID, "destination", domain.KindDestination, domain.DriverPostgres,
	)
	pipeline, err := st.CreatePipeline(context.Background(), domain.Pipeline{
		TenantID:                tenant.ID,
		Name:                    "a-to-b",
		Extractor:               "tap-postgres",
		Loader:                  "target-postgres",
		SourceConnectionID:      source.ID,
		DestinationConnectionID: destination.ID,
		DefaultTimeoutSeconds:   60,
		MaxAttempts:             1,
		Enabled:                 true,
	})
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}

	rec := doJSON(
		t,
		srv,
		http.MethodPost,
		"/v1/pipelines/"+pipeline.ID+"/runs",
		map[string]any{},
	)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status tanpa key = %d, ingin 422 (%s)", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/pipelines/"+pipeline.ID+"/runs",
		bytes.NewBufferString("{}"),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "test-key")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status dengan key = %d, ingin 202 (%s)", rec.Code, rec.Body.String())
	}
}

func TestCreatePipelinePersistsSourceFilter(t *testing.T) {
	srv, st := newTestServer(nil, "")
	tenant := createTenant(t, st, "acme")
	source := createConnection(t, st, tenant.ID, "source", domain.KindSource, domain.DriverMySQL)
	destination := createConnection(t, st, tenant.ID, "destination", domain.KindDestination, domain.DriverPostgres)

	rec := doJSON(t, srv, http.MethodPost, "/v1/pipelines", map[string]any{
		"tenant_id":                 tenant.ID,
		"name":                      "filtered-mysql",
		"extractor":                 "tap-mysql",
		"loader":                    "target-postgres",
		"source_connection_id":      source.ID,
		"destination_connection_id": destination.ID,
		"default_timeout_seconds":   60,
		"max_attempts":              1,
		"source_filter": map[string]any{
			"streams":    []string{"appdb-employees"},
			"row_filter": `status == "active"`,
		},
		"transfer_options": map[string]any{
			"batch_size_rows":  50000,
			"validate_records": false,
			"load_method":      "append-only",
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, ingin 201 (%s)", rec.Code, rec.Body.String())
	}
	filter, _ := decodeBody(t, rec)["source_filter"].(map[string]any)
	if filter["row_filter"] != `status == "active"` {
		t.Fatalf("source_filter = %#v", filter)
	}
	streams, _ := filter["streams"].([]any)
	if len(streams) != 1 || streams[0] != "appdb-employees" {
		t.Fatalf("streams = %#v", streams)
	}
	transfer, _ := decodeBody(t, rec)["transfer_options"].(map[string]any)
	if transfer["batch_size_rows"] != float64(50000) ||
		transfer["validate_records"] != false ||
		transfer["load_method"] != "append-only" {
		t.Fatalf("transfer_options = %#v", transfer)
	}
}

func TestCreatePipelineRejectsOracleRowFilter(t *testing.T) {
	srv, st := newTestServer(nil, "")
	tenant := createTenant(t, st, "acme")
	source := createConnection(t, st, tenant.ID, "source", domain.KindSource, domain.DriverOracle)
	destination := createConnection(t, st, tenant.ID, "destination", domain.KindDestination, domain.DriverPostgres)

	rec := doJSON(t, srv, http.MethodPost, "/v1/pipelines", map[string]any{
		"tenant_id":                 tenant.ID,
		"name":                      "oracle-filtered",
		"extractor":                 "tap-oracle",
		"loader":                    "target-postgres",
		"source_connection_id":      source.ID,
		"destination_connection_id": destination.ID,
		"source_filter": map[string]any{
			"streams":    []string{"APP.EMPLOYEES"},
			"row_filter": `status == "active"`,
		},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, ingin 422 (%s)", rec.Code, rec.Body.String())
	}
}
