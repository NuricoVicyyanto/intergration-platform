package connector

import (
	"testing"

	"github.com/example/controlplane/internal/domain"
)

func TestCatalogTanpaFilter(t *testing.T) {
	got := Catalog("", "")
	if len(got) != 15 {
		t.Fatalf("len(Catalog()) = %d, ingin 15", len(got))
	}
	for _, c := range got {
		if c.Installed != nil {
			t.Errorf("katalog tidak boleh mengisi Installed: %+v", c)
		}
		if c.HubURL == "" {
			t.Errorf("HubURL kosong untuk %s", c.Name)
		}
	}
}

func TestCatalogFilter(t *testing.T) {
	tests := []struct {
		name   string
		kind   domain.PluginKind
		driver domain.Driver
		want   int
	}{
		{name: "extractor saja", kind: domain.PluginExtractor, want: 8},
		{name: "loader saja", kind: domain.PluginLoader, want: 7},
		{name: "driver saja", driver: domain.DriverOracle, want: 2},
		{name: "kind + driver", kind: domain.PluginExtractor, driver: domain.DriverMySQL, want: 1},
		{name: "tidak ada yang cocok", kind: "transformer", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Catalog(tt.kind, tt.driver); len(got) != tt.want {
				t.Fatalf("len(Catalog(%q, %q)) = %d, ingin %d", tt.kind, tt.driver, len(got), tt.want)
			}
		})
	}
}

func TestCatalogMengembalikanSalinan(t *testing.T) {
	first := Catalog("", "")
	first[0].Name = "diubah"
	if Catalog("", "")[0].Name == "diubah" {
		t.Fatal("Catalog membocorkan slice internal")
	}
}

func TestDriverForPlugin(t *testing.T) {
	tests := []struct {
		name  string
		want  domain.Driver
		found bool
	}{
		{name: "tap-postgres", want: domain.DriverPostgres, found: true},
		{name: "target-mysql", want: domain.DriverMySQL, found: true},
		{name: "tap-mongodb", want: domain.DriverMongoDB, found: true},
		{name: "tap-rest-api-msdk", want: domain.DriverREST, found: true},
		{name: "target-duckdb", want: domain.DriverDuckDB, found: true},
		// Nama beralias harus dipetakan ke plugin dasarnya.
		{name: "tap-postgres--billing", want: domain.DriverPostgres, found: true},
		{name: "tap-github", found: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := DriverForPlugin(tt.name)
			if found != tt.found {
				t.Fatalf("DriverForPlugin(%q) found = %v, ingin %v", tt.name, found, tt.found)
			}
			if got != tt.want {
				t.Fatalf("DriverForPlugin(%q) = %q, ingin %q", tt.name, got, tt.want)
			}
		})
	}
}
