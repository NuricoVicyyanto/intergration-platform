// Package storefactory memilih implementasi store.Store dari konfigurasi.
//
// Paket ini sengaja terpisah dari internal/store supaya paket store tetap bebas
// dari implementasi konkret; kalau factory ditaruh di sana, store -> memory ->
// store akan menjadi siklus import.
package storefactory

import (
	"context"
	"fmt"
	"strings"

	"github.com/example/controlplane/internal/store"
	"github.com/example/controlplane/internal/store/memory"
	"github.com/example/controlplane/internal/store/postgres"
)

// MemoryDSN adalah nilai CP_DATABASE_URL yang memakai store in-memory.
// Hanya untuk demo, unit test, dan mode satu proses.
const MemoryDSN = "memory://"

// Backend mengembalikan nama backend untuk logging, tanpa membuka koneksi.
func Backend(databaseURL string) string {
	if IsMemory(databaseURL) {
		return "memory"
	}
	return "postgres"
}

// IsMemory melaporkan apakah DSN menunjuk ke store in-memory.
func IsMemory(databaseURL string) bool {
	return strings.HasPrefix(strings.TrimSpace(databaseURL), MemoryDSN)
}

// Open membuat store sesuai DSN.
//
// Nilai "memory://" memakai store in-memory; nilai lain diperlakukan sebagai
// DSN Postgres. Bila autoMigrate true, skema dijalankan lebih dulu sehingga
// proses bisa start di database kosong tanpa langkah manual.
func Open(ctx context.Context, databaseURL string, autoMigrate bool) (store.Store, error) {
	if IsMemory(databaseURL) {
		return memory.New(), nil
	}

	st, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if autoMigrate {
		if err := st.Migrate(ctx); err != nil {
			_ = st.Close()
			return nil, fmt.Errorf("migrasi skema: %w", err)
		}
	}
	return st, nil
}

// Close menutup store bila implementasinya punya resource yang perlu ditutup.
func Close(st store.Store) error {
	if closer, ok := st.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}
