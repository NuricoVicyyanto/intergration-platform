package worker

import (
	"encoding/json"
	"testing"

	"github.com/example/controlplane/internal/domain"
)

func TestConventionEnvBuilderCSV(t *testing.T) {
	env, err := (ConventionEnvBuilder{}).Build(domain.KindSource, domain.Connection{
		Driver: domain.DriverCSV,
		Endpoint: map[string]any{
			"path":     "data/orders.csv",
			"entity":   "orders",
			"keys":     []any{"id"},
			"encoding": "utf-8",
		},
	}, "")
	if err != nil {
		t.Fatalf("CSV Build: %v", err)
	}
	var files []map[string]any
	if err := json.Unmarshal([]byte(env["TAP_CSV_FILES"]), &files); err != nil {
		t.Fatalf("TAP_CSV_FILES: %v", err)
	}
	if len(files) != 1 || files[0]["entity"] != "orders" {
		t.Fatalf("files = %#v", files)
	}
}

func TestConventionEnvBuilderRESTAPI(t *testing.T) {
	env, err := (ConventionEnvBuilder{}).Build(domain.KindSource, domain.Connection{
		Driver: domain.DriverREST,
		Endpoint: map[string]any{
			"api_url":      "https://api.example.com",
			"path":         "/v1/records",
			"stream_name":  "records",
			"primary_keys": []any{"id"},
			"records_path": "$.data[*]",
			"auth_method":  "no_auth",
		},
	}, "")
	if err != nil {
		t.Fatalf("REST Build: %v", err)
	}
	var streams []map[string]any
	if err := json.Unmarshal([]byte(env["TAP_REST_API_MSDK_STREAMS"]), &streams); err != nil {
		t.Fatalf("TAP_REST_API_MSDK_STREAMS: %v", err)
	}
	if len(streams) != 1 || streams[0]["name"] != "records" ||
		streams[0]["records_path"] != "$.data[*]" {
		t.Fatalf("REST streams = %#v", streams)
	}
}

func TestConventionEnvBuilderGoogleSheets(t *testing.T) {
	secret := `{"client_id":"id","client_secret":"secret","refresh_token":"refresh"}`
	env, err := (ConventionEnvBuilder{}).Build(domain.KindSource, domain.Connection{
		Driver: domain.DriverSheets,
		Endpoint: map[string]any{
			"sheet_id":         "sheet-123",
			"child_sheet_name": "Orders",
			"range":            "A1:Z100",
		},
	}, secret)
	if err != nil {
		t.Fatalf("Sheets Build: %v", err)
	}
	if env["TAP_GOOGLE_SHEETS_SHEET_ID"] != "sheet-123" ||
		env["TAP_GOOGLE_SHEETS_OAUTH_CREDENTIALS_CLIENT_ID"] != "id" ||
		env["TAP_GOOGLE_SHEETS_OAUTH_CREDENTIALS_REFRESH_TOKEN"] != "refresh" {
		t.Fatalf("sheets env = %#v", env)
	}
}

func TestConventionEnvBuilderS3(t *testing.T) {
	env, err := (ConventionEnvBuilder{}).Build(domain.KindDestination, domain.Connection{
		Driver:   domain.DriverS3,
		Username: "access-key",
		Endpoint: map[string]any{
			"bucket": "analytics", "region": "ap-southeast-1",
			"prefix": "warehouse/", "format": "parquet",
		},
	}, "secret-key")
	if err != nil {
		t.Fatalf("S3 Build: %v", err)
	}
	if env["TARGET_S3_CLOUD_PROVIDER_AWS_AWS_BUCKET"] != "analytics" ||
		env["TARGET_S3_CLOUD_PROVIDER_AWS_AWS_ACCESS_KEY_ID"] != "access-key" ||
		env["TARGET_S3_CLOUD_PROVIDER_AWS_AWS_SECRET_ACCESS_KEY"] != "secret-key" {
		t.Fatalf("S3 env = %#v", env)
	}
}

func TestConventionEnvBuilderDuckDB(t *testing.T) {
	env, err := (ConventionEnvBuilder{}).Build(domain.KindDestination, domain.Connection{
		Driver: domain.DriverDuckDB,
		Endpoint: map[string]any{
			"path":   "output/warehouse.duckdb",
			"schema": "analytics",
		},
	}, "")
	if err != nil {
		t.Fatalf("DuckDB Build: %v", err)
	}
	if env["TARGET_DUCKDB_FILEPATH"] != "output/warehouse.duckdb" ||
		env["TARGET_DUCKDB_DEFAULT_TARGET_SCHEMA"] != "analytics" {
		t.Fatalf("DuckDB env = %#v", env)
	}
}
