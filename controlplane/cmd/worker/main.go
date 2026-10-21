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
	"github.com/example/controlplane/internal/storefactory"
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

	// Worker dan API harus menunjuk database yang sama supaya antrean job
	// benar-benar dibagi; itulah alasan CP_DATABASE_URL wajib diisi.
	openCtx, cancelOpen := context.WithTimeout(context.Background(), 30*time.Second)
	st, err := storefactory.Open(openCtx, cfg.DatabaseURL, cfg.AutoMigrate)
	cancelOpen()
	if err != nil {
		return err
	}
	defer func() { _ = storefactory.Close(st) }()
	log.Info("store siap", slog.String("backend", storefactory.Backend(cfg.DatabaseURL)))

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
		// EnvResolver membaca CP_SECRET_<REF> dari environment proses worker.
		// Cukup untuk pengembangan; di produksi ganti dengan Vault / Secrets
		// Manager supaya nilai kredensial tidak hidup di environment worker.
		Secrets: worker.EnvResolver{},
		Log:     log,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if storefactory.IsMemory(cfg.DatabaseURL) {
		log.Warn("worker memakai store in-memory; API di proses lain tidak akan melihat job ini")
	}
	return w.Run(ctx)
}
