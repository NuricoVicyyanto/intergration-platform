// Package migrations meng-embed file skema SQL control plane.
//
// File .sql di direktori ini tetap menjadi satu-satunya sumber kebenaran:
// dipakai oleh store Postgres (internal/store/postgres) dan, bila dijalankan
// lewat docker compose, dieksekusi otomatis oleh entrypoint image Postgres
// lewat mount /docker-entrypoint-initdb.d.
package migrations

import "embed"

// FS memuat seluruh file .sql di direktori ini.
//
//go:embed *.sql
var FS embed.FS
