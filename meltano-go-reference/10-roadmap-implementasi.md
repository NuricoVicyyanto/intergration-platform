> Bagian 10 dari 16 - [Daftar Isi](README.md)

## 10. Roadmap Implementasi Bertahap

### Fase 0 - Fondasi (tanpa replikasi)

1. `go.mod`, layout `cmd/`+`internal/`, `log/slog` JSON handler.
2. Interface `Source`, `Sink`, `StateStore`, `Queue` di `internal/domain` + `internal/connector`. Tulis
   **fake in-memory implementation** lebih dulu.
3. Metadata Postgres + migrasi + `sqlc`: tabel `jobs`, `job_runs`, `checkpoints`, `connections`.
4. Uji end-to-end dengan source in-memory -> sink in-memory, agar seluruh state machine teruji tanpa
   database nyata.

### Fase 1 - MVP: full load + incremental satu jalur

5. Source Postgres (full load dengan keyset pagination + watermark untuk incremental).
6. Sink Postgres (staging table + `INSERT ... ON CONFLICT` upsert).
7. Checkpoint: tulis dalam **transaksi yang sama** dengan commit batch ke destination.
8. Worker tunggal in-process, `context.Context` di semua jalur, graceful shutdown pada SIGTERM.
9. CLI/API minimal: `POST /jobs` (spec), `GET /jobs/{id}`, `POST /jobs/{id}/cancel`.

Kriteria keluar Fase 1: kill -9 di tengah run -> run ulang menghasilkan **nol duplikasi** untuk tabel dengan
primary key. Ini adalah kemampuan yang Meltano **tidak** berikan.

### Fase 2 - Skala & reliability

10. Queue dengan klaim atomik (`FOR UPDATE SKIP LOCKED`), worker pool terpisah dari API.
11. Heartbeat + deteksi worker mati + requeue dengan batas percobaan.
12. Retry berklasifikasi: `TransientError` (network, deadlock, serialization failure) vs `PermanentError`
    (schema mismatch, permission denied).
13. Timeout per job dan per query; progres job yang observable.
14. Metrics Prometheus: `rows_processed_total`, `checkpoint_age_seconds`, `replication_lag_seconds`,
    `job_duration_seconds`, `retry_total`.

### Fase 3 - Multi-database

15. Source MySQL (binlog reader atau watermark-based) - mulai dengan incremental watermark, baru CDC.
16. Sink MySQL. Ekstrak `TypeMapping` per dialect agar konversi tipe bisa diuji terpisah.
17. Source SQL Server (change tracking sebagai langkah awal).
18. Source Oracle (LogMiner atau watermark; pertimbangkan lisensi driver `godror` vs `go-ora`).
19. Test matrix lintas dialect dengan testcontainers.

### Fase 4 - Operasional

20. Scheduled job (cron) dengan idempotency key.
21. UI/dashboard status, API log streaming.
22. Backfill & re-sync per-tabel tanpa mengganggu incremental berjalan.
23. Multi-tenant isolation (kuota per tenant, batas concurrency per connection).
24. Schema evolution: deteksi DDL, DDL apply opsional ke destination.

---

**Navigasi:** [Bagian 9](09-struktur-direktori-go.md) - [Daftar Isi](README.md) - [Bagian 11](11-risiko-dan-keputusan.md)
