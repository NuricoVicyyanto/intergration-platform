package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/example/controlplane/internal/domain"
)

func TestCRUDTenantConnectionPipeline(t *testing.T) {
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
		DefaultTimeoutSeconds:   3600,
		MaxAttempts:             3,
		Enabled:                 true,
	})
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}

	rec := doJSON(t, srv, http.MethodPatch, "/v1/tenants/"+tenant.ID, map[string]any{
		"display_name": "Acme production",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch tenant = %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeBody(t, rec)["display_name"]; got != "Acme production" {
		t.Fatalf("display_name = %v", got)
	}

	rec = doJSON(t, srv, http.MethodPatch, "/v1/connections/"+source.ID, map[string]any{
		"name":     "orders-source",
		"username": "reporter",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch connection = %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeBody(t, rec)["name"]; got != "orders-source" {
		t.Fatalf("connection name = %v", got)
	}

	rec = doJSON(t, srv, http.MethodPatch, "/v1/pipelines/"+pipeline.ID, map[string]any{
		"name":    "orders-to-backup",
		"enabled": false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch pipeline = %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["name"] != "orders-to-backup" || body["enabled"] != false {
		t.Fatalf("pipeline hasil patch tidak sesuai: %#v", body)
	}

	if rec := doJSON(t, srv, http.MethodDelete, "/v1/connections/"+source.ID, nil); rec.Code != http.StatusConflict {
		t.Fatalf("delete koneksi yang dipakai = %d, ingin 409 (%s)", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, srv, http.MethodDelete, "/v1/pipelines/"+pipeline.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete pipeline = %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, srv, http.MethodDelete, "/v1/connections/"+source.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete source = %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, srv, http.MethodDelete, "/v1/connections/"+destination.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete destination = %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, srv, http.MethodDelete, "/v1/tenants/"+tenant.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete tenant = %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := doGet(t, srv, "/v1/tenants/"+tenant.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("tenant setelah delete = %d, ingin 404", rec.Code)
	}
}
