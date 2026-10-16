// Package domain berisi tipe inti control plane, tanpa dependency infrastruktur.
package domain

import (
	"errors"
	"time"
)

// Error sentinel yang dipakai lapisan store dan API.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrInvalid  = errors.New("invalid")
)

// RunStatus adalah status yang dikelola control plane.
//
// Pemetaannya ke sisi Meltano (lihat meltano-go-reference/13 bagian 13.5.4 dan 14 bagian 14.2.1):
//
//	queued     -> belum ada baris `runs` di Meltano
//	running    -> runs.state = RUNNING
//	succeeded  -> exit code 0
//	failed     -> exit code 1
//	cancelled  -> exit code 143 (SIGTERM)
//	timed_out  -> exit code 1 + penanda "Run timed out" pada log
//	lost       -> lease kedaluwarsa sebelum worker melaporkan hasil
type RunStatus string

const (
	StatusQueued     RunStatus = "queued"
	StatusRunning    RunStatus = "running"
	StatusCancelling RunStatus = "cancelling"
	StatusSucceeded  RunStatus = "succeeded"
	StatusFailed     RunStatus = "failed"
	StatusCancelled  RunStatus = "cancelled"
	StatusTimedOut   RunStatus = "timed_out"
	StatusLost       RunStatus = "lost"
)

// IsTerminal melaporkan apakah status tidak akan berubah lagi.
func (s RunStatus) IsTerminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusCancelled, StatusTimedOut:
		return true
	default:
		return false
	}
}

// IsActive melaporkan apakah status masih memegang "slot" pipeline.
// Index unik parsial di migrations/0001_init.sql memakai himpunan yang sama.
func (s RunStatus) IsActive() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusCancelling:
		return true
	default:
		return false
	}
}

// Valid melaporkan apakah nilai termasuk enumerasi yang dikenal.
func (s RunStatus) Valid() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusCancelling, StatusSucceeded,
		StatusFailed, StatusCancelled, StatusTimedOut, StatusLost:
		return true
	default:
		return false
	}
}

// ConnectionKind membedakan sumber dan tujuan.
type ConnectionKind string

const (
	KindSource      ConnectionKind = "source"
	KindDestination ConnectionKind = "destination"
)

// Driver adalah jenis database yang didukung secara bertahap (lihat roadmap bagian 10).
type Driver string

const (
	DriverPostgres Driver = "postgres"
	DriverMySQL    Driver = "mysql"
	DriverMSSQL    Driver = "mssql"
	DriverOracle   Driver = "oracle"
)

// Tenant mewakili satu direktori project Meltano.
type Tenant struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	DisplayName     string    `json:"display_name,omitempty"`
	EnvironmentName string    `json:"environment_name"`
	ProjectDir      string    `json:"project_dir"`
	CreatedAt       time.Time `json:"created_at"`
}

// Connection hanya menyimpan referensi kredensial, bukan nilainya.
// Worker yang me-resolve secret_ref saat spawn (lihat meltano-go-reference/13 bagian 13.9.1).
type Connection struct {
	ID        string         `json:"id"`
	TenantID  string         `json:"tenant_id"`
	Name      string         `json:"name"`
	Kind      ConnectionKind `json:"kind"`
	Driver    Driver         `json:"driver"`
	Endpoint  map[string]any `json:"endpoint"`
	Username  string         `json:"username,omitempty"`
	SecretRef string         `json:"secret_ref"`
	Options   map[string]any `json:"options,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// Pipeline adalah definisi yang hidup di database control plane.
// `meltano.yml` hanya memuat katalog plugin dan environment, bukan definisi pipeline.
type Pipeline struct {
	ID                      string    `json:"id"`
	TenantID                string    `json:"tenant_id"`
	Name                    string    `json:"name"`
	Extractor               string    `json:"extractor"`
	Loader                  string    `json:"loader"`
	SourceConnectionID      string    `json:"source_connection_id"`
	DestinationConnectionID string    `json:"destination_connection_id"`
	StateIDSuffix           *string   `json:"state_id_suffix"`
	DefaultTimeoutSeconds   int       `json:"default_timeout_seconds"`
	MaxAttempts             int       `json:"max_attempts"`
	Enabled                 bool      `json:"enabled"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

// Run adalah satu eksekusi pipeline.
type Run struct {
	ID           string    `json:"id"`
	PipelineID   string    `json:"pipeline_id"`
	Status       RunStatus `json:"status"`
	Attempt      int       `json:"attempt"`
	AutoAttempts int       `json:"auto_attempts"`
	RetriedFrom  *string   `json:"retried_from"`
	Trigger      string    `json:"trigger"`
	Reason       string    `json:"reason,omitempty"`

	// Korelasi ke sisi Meltano.
	MeltanoStateID string `json:"-"`
	MeltanoRunID   string `json:"-"`

	ExitCode *int   `json:"exit_code"`
	Error    string `json:"error,omitempty"`
	LogTail  string `json:"-"`

	ClaimedBy      *string    `json:"-"`
	LeaseExpiresAt *time.Time `json:"-"`

	TimeoutSeconds int     `json:"timeout_seconds"`
	FullRefresh    bool    `json:"full_refresh"`
	StateIDSuffix  *string `json:"state_id_suffix"`

	CreatedAt time.Time  `json:"created_at"`
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
}

// DurationSeconds mengembalikan durasi run dalam detik, atau nil bila belum selesai.
func (r Run) DurationSeconds() *float64 {
	if r.StartedAt == nil || r.EndedAt == nil {
		return nil
	}
	d := r.EndedAt.Sub(*r.StartedAt).Seconds()
	return &d
}

// RunEvent adalah jejak terstruktur sebuah run.
type RunEvent struct {
	ID      int64          `json:"-"`
	RunID   string         `json:"-"`
	At      time.Time      `json:"at"`
	Kind    string         `json:"kind"`
	Payload map[string]any `json:"payload"`
}
