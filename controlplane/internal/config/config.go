// Package config memuat konfigurasi control plane dari environment variable.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config adalah konfigurasi proses. Semua bersumber dari environment variable
// dengan prefix CP_ (lihat env.example).
type Config struct {
	HTTPAddr string

	// DatabaseURL adalah job store control plane (milik Anda, bukan Meltano).
	DatabaseURL string

	// MeltanoBin adalah path executable Meltano. Kosong berarti "meltano" dari PATH.
	MeltanoBin string
	// TenantsRoot adalah direktori induk yang memuat satu project Meltano per tenant.
	TenantsRoot string
	// SystemDBURI adalah MELTANO_DATABASE_URI yang diinjeksi ke proses Meltano.
	systemDBURI string

	WorkerConcurrency int
	JobTimeoutSeconds int
	HeartbeatSeconds  int
	LeaseSeconds      int
	LogTailLines      int
	LogLevel          string
	// EmbeddedWorker menjalankan worker di dalam proses API dengan store yang
	// sama. Dipakai untuk demo dan pengembangan sebelum store Postgres ada
	// (lihat README dan doc 16 bagian 16.5).
	EmbeddedWorker bool
}

// SystemDBURI mengembalikan URI system database Meltano.
// Default SQLite tidak aman untuk multi-worker (lihat doc 15 bagian 15.7.2),
// jadi nilai ini sebaiknya selalu diarahkan ke Postgres bersama di produksi.
func (c Config) SystemDBURI() string { return c.systemDBURI }

// Load membaca konfigurasi dan memvalidasi nilai yang wajib.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:          env("CP_HTTP_ADDR", ":8080"),
		DatabaseURL:       env("CP_DATABASE_URL", ""),
		MeltanoBin:        env("CP_MELTANO_BIN", "meltano"),
		TenantsRoot:       env("CP_TENANTS_ROOT", "/srv/tenants"),
		systemDBURI:       env("CP_SYSTEM_DB_URI", ""),
		WorkerConcurrency: envInt("CP_WORKER_CONCURRENCY", 2),
		JobTimeoutSeconds: envInt("CP_JOB_TIMEOUT_SECONDS", 3600),
		HeartbeatSeconds:  envInt("CP_HEARTBEAT_SECONDS", 10),
		LeaseSeconds:      envInt("CP_LEASE_SECONDS", 300),
		LogTailLines:      envInt("CP_LOG_TAIL_LINES", 100),
		LogLevel:          env("CP_LOG_LEVEL", "info"),
		EmbeddedWorker:    envBool("CP_EMBEDDED_WORKER", false),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("CP_DATABASE_URL wajib diisi")
	}
	if cfg.WorkerConcurrency < 1 {
		return Config{}, fmt.Errorf("CP_WORKER_CONCURRENCY harus >= 1")
	}
	if cfg.JobTimeoutSeconds < 1 {
		return Config{}, fmt.Errorf("CP_JOB_TIMEOUT_SECONDS harus >= 1")
	}
	// Heartbeat harus jauh lebih pendek daripada lease, kalau tidak run akan
	// dianggap yatim saat masih berjalan.
	if cfg.HeartbeatSeconds < 1 || cfg.HeartbeatSeconds*3 > cfg.LeaseSeconds {
		return Config{}, fmt.Errorf(
			"CP_HEARTBEAT_SECONDS harus >= 1 dan tidak lebih dari sepertiga CP_LEASE_SECONDS")
	}
	return cfg, nil
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "":
		return def
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
