package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/example/controlplane/internal/domain"
	"github.com/example/controlplane/internal/meltano"
	"github.com/example/controlplane/internal/store/memory"
)

type stubLister struct {
	plugins []meltano.Plugin
	err     error
	dir     string
}

func (s *stubLister) ListPlugins(_ context.Context, dir string) ([]meltano.Plugin, error) {
	s.dir = dir
	return s.plugins, s.err
}

func newTestServer(lister ConnectorLister, tenantsRoot string) (*Server, *memory.Store) {
	st := memory.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(st, log, "test")
	if lister != nil {
		WithConnectorLister(lister, tenantsRoot)(srv)
	}
	return srv, st
}

func doGet(t *testing.T, srv *Server, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body bukan JSON: %v (%s)", err, rec.Body.String())
	}
	return body
}

func newTenant(t *testing.T, st *memory.Store, projectDir string) domain.Tenant {
	t.Helper()
	tenant, err := st.CreateTenant(context.Background(), domain.Tenant{
		Name: "acme", EnvironmentName: "acme", ProjectDir: projectDir,
	})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	return tenant
}

func TestListConnectorsKatalog(t *testing.T) {
	srv, _ := newTestServer(nil, "")
	rec := doGet(t, srv, "/v1/connectors")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, ingin 200 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["source"] != "catalog" {
		t.Fatalf("source = %v, ingin catalog", body["source"])
	}
	data, _ := body["data"].([]any)
	if len(data) != 15 {
		t.Fatalf("len(data) = %d, ingin 15", len(data))
	}
	first, _ := data[0].(map[string]any)
	if _, ok := first["installed"]; ok {
		t.Fatalf("mode katalog tidak boleh mengisi installed: %v", first)
	}
}

func TestListConnectorsFilter(t *testing.T) {
	srv, _ := newTestServer(nil, "")
	rec := doGet(t, srv, "/v1/connectors?kind=extractor&driver=mysql")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, ingin 200", rec.Code)
	}
	data, _ := decodeBody(t, rec)["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("len(data) = %d, ingin 1", len(data))
	}
	item, _ := data[0].(map[string]any)
	if item["name"] != "tap-mysql" {
		t.Fatalf("name = %v, ingin tap-mysql", item["name"])
	}
}

func TestListConnectorsKindTidakDikenal(t *testing.T) {
	srv, _ := newTestServer(nil, "")
	if rec := doGet(t, srv, "/v1/connectors?kind=transformer"); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, ingin 422", rec.Code)
	}
}

func TestListConnectorsTenantMenandaiInstalled(t *testing.T) {
	lister := &stubLister{plugins: []meltano.Plugin{
		{Type: "extractor", Name: "tap-postgres", Variant: "meltanolabs"},
		{Type: "loader", Name: "target-github"}, // tidak ada di katalog, driver kosong
		{Type: "utility", Name: "dbt-postgres"}, // bukan extractor/loader, harus diabaikan
	}}
	srv, st := newTestServer(lister, "")
	tenant := newTenant(t, st, "/srv/tenants/acme")

	rec := doGet(t, srv, "/v1/connectors?tenant_id="+tenant.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, ingin 200 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["source"] != "tenant" {
		t.Fatalf("source = %v, ingin tenant", body["source"])
	}
	if lister.dir != "/srv/tenants/acme" {
		t.Fatalf("project dir = %q, ingin ProjectDir tenant", lister.dir)
	}

	data, _ := body["data"].([]any)
	if len(data) != 16 { // 15 katalog + target-github
		t.Fatalf("len(data) = %d, ingin 16", len(data))
	}
	installed := 0
	var custom map[string]any
	for _, raw := range data {
		item, _ := raw.(map[string]any)
		if item["installed"] == true {
			installed++
		}
		if item["name"] == "target-github" {
			custom = item
		}
	}
	if installed != 2 {
		t.Fatalf("installed = %d, ingin 2", installed)
	}
	if custom == nil {
		t.Fatal("plugin tenant yang tidak ada di katalog tidak ikut dikembalikan")
	}
	if _, ok := custom["driver"]; ok {
		t.Fatalf("driver harus kosong untuk plugin non-database: %v", custom)
	}
}

func TestCreateFileConnectionsWithoutDatabaseCredentials(t *testing.T) {
	srv, st := newTestServer(nil, "")
	tenant := createTenant(t, st, "file-connectors")
	for _, test := range []struct {
		driver   domain.Driver
		kind     domain.ConnectionKind
		endpoint map[string]any
	}{
		{
			driver: domain.DriverCSV,
			kind:   domain.KindSource,
			endpoint: map[string]any{
				"path": "data/orders.csv", "entity": "orders",
			},
		},
		{
			driver: domain.DriverDuckDB,
			kind:   domain.KindDestination,
			endpoint: map[string]any{
				"path": "output/warehouse.duckdb",
			},
		},
	} {
		rec := doJSON(t, srv, http.MethodPost, "/v1/connections", map[string]any{
			"tenant_id": tenant.ID,
			"name":      string(test.driver),
			"kind":      test.kind,
			"driver":    test.driver,
			"endpoint":  test.endpoint,
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s = %d (%s)", test.driver, rec.Code, rec.Body.String())
		}
	}
}

func TestListConnectorsTenantPakaiTenantsRoot(t *testing.T) {
	lister := &stubLister{}
	srv, st := newTestServer(lister, filepath.Join("srv", "tenants"))
	tenant := newTenant(t, st, "")

	if rec := doGet(t, srv, "/v1/connectors?tenant_id="+tenant.ID); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, ingin 200", rec.Code)
	}
	if filepath.Base(lister.dir) != "acme" {
		t.Fatalf("project dir = %q, ingin berakhir dengan nama tenant", lister.dir)
	}
}

func TestListConnectorsTenantTanpaProjectDir(t *testing.T) {
	srv, st := newTestServer(&stubLister{}, "")
	tenant := newTenant(t, st, "")
	if rec := doGet(t, srv, "/v1/connectors?tenant_id="+tenant.ID); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, ingin 422", rec.Code)
	}
}

func TestListConnectorsTenantError(t *testing.T) {
	lister := &stubLister{err: errors.New("meltano plugin list gagal (exit 1): no meltano.yml")}
	srv, st := newTestServer(lister, "")
	tenant := newTenant(t, st, "/srv/tenants/acme")
	if rec := doGet(t, srv, "/v1/connectors?tenant_id="+tenant.ID); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, ingin 500", rec.Code)
	}
}

func TestListConnectorsTenantTanpaLister(t *testing.T) {
	srv, st := newTestServer(nil, "")
	tenant := newTenant(t, st, "/srv/tenants/acme")
	if rec := doGet(t, srv, "/v1/connectors?tenant_id="+tenant.ID); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, ingin 500", rec.Code)
	}
}

func TestListConnectorsTenantTidakAda(t *testing.T) {
	srv, _ := newTestServer(&stubLister{}, "")
	if rec := doGet(t, srv, "/v1/connectors?tenant_id=tidak-ada"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, ingin 404", rec.Code)
	}
}
