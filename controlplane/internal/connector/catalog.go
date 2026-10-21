// Package connector memuat katalog konektor yang dikenal control plane.
//
// Katalog ini sengaja statis, tanpa dependensi jaringan: fungsinya hanya
// memberi tahu UI nama plugin Meltano yang bisa dipilih pada field `extractor`
// dan `loader` pipeline. Yang benar-benar memindahkan data tetap plugin yang
// terpasang di project Meltano tenant (lihat docs/menambah-konektor.md jalur 0).
//
// Apakah plugin sudah terpasang di sebuah tenant TIDAK diketahui dari katalog
// ini; itu dibaca dari `meltano plugin list --format=json` (mode tenant pada
// GET /v1/connectors).
package connector

import (
	"strings"

	"github.com/example/controlplane/internal/domain"
)

// hubBase adalah basis URL halaman plugin di Meltano Hub.
const hubBase = "https://hub.meltano.com"

// entry adalah satu baris katalog. Variant sengaja tidak dicantumkan supaya
// katalog tidak mengunci variant default yang bisa berubah di Hub; variant
// sebenarnya dilaporkan mode tenant.
type entry struct {
	name   string
	kind   domain.PluginKind
	driver domain.Driver
	label  string
}

var catalog = []entry{
	{name: "tap-postgres", kind: domain.PluginExtractor, driver: domain.DriverPostgres, label: "PostgreSQL (extractor)"},
	{name: "target-postgres", kind: domain.PluginLoader, driver: domain.DriverPostgres, label: "PostgreSQL (loader)"},
	{name: "tap-mysql", kind: domain.PluginExtractor, driver: domain.DriverMySQL, label: "MySQL (extractor)"},
	{name: "target-mysql", kind: domain.PluginLoader, driver: domain.DriverMySQL, label: "MySQL (loader)"},
	{name: "tap-mssql", kind: domain.PluginExtractor, driver: domain.DriverMSSQL, label: "MS SQL Server (extractor)"},
	{name: "target-mssql", kind: domain.PluginLoader, driver: domain.DriverMSSQL, label: "MS SQL Server (loader)"},
	{name: "tap-oracle", kind: domain.PluginExtractor, driver: domain.DriverOracle, label: "Oracle (extractor)"},
	{name: "target-oracle", kind: domain.PluginLoader, driver: domain.DriverOracle, label: "Oracle (loader)"},
	{name: "tap-mongodb", kind: domain.PluginExtractor, driver: domain.DriverMongoDB, label: "MongoDB (extractor)"},
	{name: "target-mongodb", kind: domain.PluginLoader, driver: domain.DriverMongoDB, label: "MongoDB (loader)"},
	{name: "tap-rest-api-msdk", kind: domain.PluginExtractor, driver: domain.DriverREST, label: "REST API (extractor)"},
	{name: "tap-csv", kind: domain.PluginExtractor, driver: domain.DriverCSV, label: "CSV (extractor)"},
	{name: "tap-google-sheets", kind: domain.PluginExtractor, driver: domain.DriverSheets, label: "Google Sheets (extractor)"},
	{name: "target-s3", kind: domain.PluginLoader, driver: domain.DriverS3, label: "Amazon S3 (loader)"},
	{name: "target-duckdb", kind: domain.PluginLoader, driver: domain.DriverDuckDB, label: "DuckDB (loader)"},
}

// pluginDrivers memetakan nama plugin ke label driver. Dipakai untuk menandai
// plugin yang sudah terpasang di tenant, termasuk nama beralias
// (mis. tap-postgres--billing).
var pluginDrivers = func() map[string]domain.Driver {
	m := make(map[string]domain.Driver, len(catalog))
	for _, e := range catalog {
		m[e.name] = e.driver
	}
	return m
}()

// Catalog mengembalikan katalog konektor, difilter bila kind atau driver diisi.
// Nilai kosong berarti tanpa filter. Hasilnya selalu salinan baru.
func Catalog(kind domain.PluginKind, driver domain.Driver) []domain.Connector {
	out := make([]domain.Connector, 0, len(catalog))
	for _, e := range catalog {
		if kind != "" && e.kind != kind {
			continue
		}
		if driver != "" && e.driver != driver {
			continue
		}
		out = append(out, domain.Connector{
			Name:   e.name,
			Kind:   e.kind,
			Driver: e.driver,
			Label:  e.label,
			HubURL: hubBase + "/" + string(e.kind) + "s/" + e.name,
		})
	}
	return out
}

// DriverForPlugin mengembalikan driver untuk sebuah nama plugin Meltano.
//
// Nama beralias (`<nama>--<alias>`) dipetakan ke plugin dasarnya. Nama yang
// tidak dikenal mengembalikan false; itu bukan error, karena tenant boleh
// memasang plugin apa pun, termasuk connector non-database.
func DriverForPlugin(name string) (domain.Driver, bool) {
	base, _, _ := strings.Cut(name, "--")
	driver, ok := pluginDrivers[base]
	return driver, ok
}

// LabelForPlugin mengembalikan label katalog bila plugin dikenal.
func LabelForPlugin(name string) (string, bool) {
	base, _, _ := strings.Cut(name, "--")
	for _, e := range catalog {
		if e.name == base {
			return e.label, true
		}
	}
	return "", false
}
