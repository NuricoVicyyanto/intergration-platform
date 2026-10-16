// Command control-plane menjalankan HTTP API control plane.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/example/controlplane/internal/api"
	"github.com/example/controlplane/internal/config"
	"github.com/example/controlplane/internal/meltano"
	"github.com/example/controlplane/internal/obs"
	"github.com/example/controlplane/internal/store/memory"
	"github.com/example/controlplane/internal/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Default().Error("control plane berhenti dengan error", slog.Any("error", err))
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

	// Store Postgres belum diimplementasikan; in-memory dipakai untuk
	// menjalankan API tanpa database (lihat README).
	st := memory.New()

	srv := api.New(st, log, detectMeltanoVersion(cfg.MeltanoBin))

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Worker tertanam: berbagi store yang sama dengan API sehingga alur
	// POST /runs -> eksekusi -> status terminal bisa dicoba dalam satu proses.
	// Hanya untuk demo; di produksi jalankan cmd/worker terpisah dengan store
	// Postgres bersama.
	if cfg.EmbeddedWorker {
		host, _ := os.Hostname()
		w := &worker.Worker{
			Cfg: worker.Config{
				WorkerID:     host + "-embedded",
				TenantsRoot:  cfg.TenantsRoot,
				SystemDBURI:  cfg.SystemDBURI(),
				Lease:        time.Duration(cfg.LeaseSeconds) * time.Second,
				Heartbeat:    time.Duration(cfg.HeartbeatSeconds) * time.Second,
				LogTailLines: cfg.LogTailLines,
			},
			Store:   st,
			Adapter: &meltano.Adapter{Bin: cfg.MeltanoBin, Logger: log},
			Secrets: worker.EnvResolver{},
			Log:     log,
		}
		log.Warn("worker tertanam aktif (demo); produksi memakai store Postgres + cmd/worker")
		go func() {
			if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Error("worker tertanam berhenti", slog.Any("error", err))
			}
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("control plane mendengarkan", slog.String("addr", cfg.HTTPAddr))
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("sinyal berhenti diterima, menutup server")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

// detectMeltanoVersion menjalankan `meltano --version` sekali saat startup.
// Nilainya dilaporkan oleh GET /v1/version supaya mismatch versi terdeteksi
// sebelum job gagal dengan exit code 3.
func detectMeltanoVersion(bin string) string {
	if bin == "" {
		bin = "meltano"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "unknown"
	}
	// Contoh keluaran: "meltano, version 4.4.1"
	s := strings.TrimSpace(string(out))
	if i := strings.LastIndex(s, " "); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}
