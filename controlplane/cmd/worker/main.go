// Command worker menjalankan loop eksekusi job.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/example/controlplane/internal/config"
	"github.com/example/controlplane/internal/meltano"
	"github.com/example/controlplane/internal/obs"
	"github.com/example/controlplane/internal/store/memory"
	"github.com/example/controlplane/internal/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Default().Error("worker berhenti dengan error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := obs.NewLogger(cfg.LogLevel)
	slog.SetDefault(log)

	// Store Postgres belum diimplementasikan. Worker dengan store in-memory
	// hanya berguna untuk pengembangan; produksi memerlukan store bersama.
	st := memory.New()

	hostname, _ := os.Hostname()
	workerID := hostname + "-" + strconv.Itoa(os.Getpid())

	w := &worker.Worker{
		Cfg: worker.Config{
			WorkerID:     workerID,
			TenantsRoot:  cfg.TenantsRoot,
			SystemDBURI:  cfg.SystemDBURI(),
			Lease:        time.Duration(cfg.LeaseSeconds) * time.Second,
			Heartbeat:    time.Duration(cfg.HeartbeatSeconds) * time.Second,
			LogTailLines: cfg.LogTailLines,
		},
		Store:   st,
		Adapter: &meltano.Adapter{Bin: cfg.MeltanoBin, Logger: log},
		Log:     log,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Warn("worker memakai store in-memory; gunakan implementasi Postgres untuk produksi")
	return w.Run(ctx)
}
