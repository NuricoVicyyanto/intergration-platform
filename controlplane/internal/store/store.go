// Package store mendefinisikan persistensi job control plane.
//
// Implementasi Postgres ditambahkan menyusul (lihat README bagian "Berikutnya").
// Paket memory dipakai untuk test dan untuk menjalankan API tanpa database.
package store

import (
	"context"
	"time"

	"github.com/example/controlplane/internal/domain"
)

// RunFilter menyaring daftar run.
type RunFilter struct {
	PipelineID string
	Statuses   []domain.RunStatus
	Limit      int
}

// Completion adalah hasil terminal sebuah run.
//
// ExitCode memakai pointer agar nilai 0 dapat dibedakan dari "belum ada".
type Completion struct {
	Status         domain.RunStatus
	ExitCode       *int
	Error          string
	LogTail        string
	MeltanoStateID string
	MeltanoRunID   string
	AutoAttempts   int
}

// Store adalah kontrak persistensi yang dipakai API dan worker.
type Store interface {
	// --- Tenant ---

	CreateTenant(ctx context.Context, tenant domain.Tenant) (domain.Tenant, error)
	GetTenant(ctx context.Context, id string) (domain.Tenant, error)
	ListTenants(ctx context.Context) ([]domain.Tenant, error)
	UpdateTenant(ctx context.Context, tenant domain.Tenant) (domain.Tenant, error)
	DeleteTenant(ctx context.Context, id string) error

	// --- Connection ---

	CreateConnection(ctx context.Context, conn domain.Connection) (domain.Connection, error)
	GetConnection(ctx context.Context, id string) (domain.Connection, error)
	ListConnections(ctx context.Context, tenantID string) ([]domain.Connection, error)
	UpdateConnection(ctx context.Context, conn domain.Connection) (domain.Connection, error)
	DeleteConnection(ctx context.Context, id string) error

	// --- Pipeline ---

	CreatePipeline(ctx context.Context, p domain.Pipeline) (domain.Pipeline, error)
	GetPipeline(ctx context.Context, id string) (domain.Pipeline, error)
	ListPipelines(ctx context.Context, tenantID string) ([]domain.Pipeline, error)
	UpdatePipeline(ctx context.Context, p domain.Pipeline) (domain.Pipeline, error)
	DeletePipeline(ctx context.Context, id string) error

	// --- Run ---

	// CreateRun menyimpan run baru berstatus queued.
	//
	// Mengembalikan domain.ErrConflict bila pipeline sudah punya run aktif
	// (menggantikan pengaman advisory Meltano, doc 13 bagian 13.10), atau bila
	// idempotencyKey sudah pernah dipakai dengan isi yang berbeda.
	CreateRun(ctx context.Context, run domain.Run, idempotencyKey string) (domain.Run, error)
	GetRun(ctx context.Context, id string) (domain.Run, error)
	ListRuns(ctx context.Context, filter RunFilter) ([]domain.Run, error)

	// ClaimRun mengambil satu run queued secara atomik dan menandainya running.
	// Mengembalikan (nil, nil) bila antrean kosong.
	ClaimRun(ctx context.Context, workerID string, lease time.Duration) (*domain.Run, error)

	// CompleteRun menulis hasil terminal.
	CompleteRun(ctx context.Context, id string, c Completion) (domain.Run, error)

	// HeartbeatRun memperpanjang lease run yang sedang berjalan.
	HeartbeatRun(ctx context.Context, id, workerID string, lease time.Duration) error

	// RequestCancel menandai run untuk dibatalkan. Idempoten.
	RequestCancel(ctx context.Context, id string) (domain.Run, error)

	// ReapExpired menandai run yang lease-nya habis sebagai lost lalu failed,
	// dan melaporkan berapa baris yang tersentuh. Worker yang mati tidak bisa
	// mengirim sinyal apa pun, sehingga rekonsiliasi ini wajib ada (doc 13 13.10).
	ReapExpired(ctx context.Context, now time.Time) (int, error)

	// --- Event ---

	AppendEvent(ctx context.Context, ev domain.RunEvent) error
	ListEvents(ctx context.Context, runID string, limit int) ([]domain.RunEvent, error)
}
