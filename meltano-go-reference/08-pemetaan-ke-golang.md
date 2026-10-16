> Bagian 8 dari 16 - [Daftar Isi](README.md)

## 8. Pemetaan Konsep Meltano ke Arsitektur Golang

> **Mulai di sini: rekomendasi desain, bukan temuan source code.** Setiap baris memisahkan padanan,
> paket Go yang sesuai, trade-off, dan risiko.

| # | Komponen Go | Tanggung jawab | Padanan Meltano | Paket/interface Go | Trade-off | Risiko |
|---|---|---|---|---|---|---|
| 1 | **Control Plane API** | Terima definisi job, validasi, enqueue, lacak status | `cli/run.py` + `Job` + `ProjectPluginsService` | `net/http` + `chi`; `internal/api`; `type JobStore interface` | Pemisahan control/data plane memungkinkan scaling, tapi menambah hop jaringan | Konsistensi state antara control plane dan worker saat worker tak terjangkau |
| 2 | **Job Store / Metadata DB** | Simpan definisi job, run history, status, heartbeat | Tabel `runs` + `state` (SQLAlchemy) | `pgx`/`sqlc`; `internal/store` | Postgres memberi transaksi dan `SELECT ... FOR UPDATE SKIP LOCKED` | Jangan meniru query-based advisory check (`extract_load.py:527`) - pakai lock nyata |
| 3 | **Scheduler / Dispatcher** | Poll queue, klaim job, atur concurrency | `ScheduleService.run` (hanya shell-out) | `Redis`/`NATS` atau `Postgres LISTEN/NOTIFY`; `internal/scheduler` | Redis cepat tapi state tambahan; Postgres lebih sederhana untuk konsistensi | Meltano hanya punya cron *expression* tanpa daemon; ini bagian yang **harus dirancang ulang** |
| 4 | **Worker Pool** | Jalankan replikasi, batasi resource | `PluginInstallService.parallelism` + Semaphore | `golang.org/x/sync/semaphore`, `errgroup` | Semaphore proses-lokal tidak membatasi lintas worker | Overcommit CPU/memori lintas node; butuh quota di level klaim job |
| 5 | **Source Connector** | Baca data sumber, kirim batch | `SingerTap` (`--config`, `--state`) | `type Source interface { Discover(ctx) (Schema, error); Read(ctx, State, chan<- Batch) error }` | Interface in-process jauh lebih cepat dari subprocess Singer, tapi kehilangan ekosistem 300+ tap | Implementasi per-DB (Postgres/MySQL/SQL Server/Oracle) adalah biaya terbesar proyek |
| 6 | **Replication Engine** | Orkestrasi, urutan, checkpoint, error policy | `ELBExecutionManager` + `_link_io` | `internal/replication`; pipeline berbasis channel | Channel Go mengubah backpressure menjadi persoalan desain; Meltano menghindarinya lewat pipe OS | Deadlock/goroutine leak bila consumer mati; wajib `context.Context` di semua jalur |
| 7 | **Destination Connector** | Tulis batch, idempotent upsert | `SingerTarget` (`BookmarkWriter`) | `type Sink interface { Write(ctx, Batch) error; Flush(ctx) error; Commit(ctx, State) error }` | Transaksi per-batch memberi exactly-once *di dalam* batch, tapi butuh staging table | Tanpa `Commit` terpisah, duplikasi data tetap terjadi (masalah yang sama seperti Meltano) |
| 8 | **State Store** | Simpan checkpoint durable dengan versi | `StateStoreManager` + `MeltanoState(partial/completed)` | `internal/state`; `type StateStore interface { Get(ctx, id) (State, error); Commit(ctx, id, State, version) error }` | **Optimistic concurrency (`version`/ETag)** memperbaiki celah lock naive Meltano | Lock berbasis timestamp (Meltano) rawan race di object storage; pakai conditional write |
| 9 | **Checkpoint Policy** | Tentukan kapan checkpoint diambil | Implisit: setiap STATE dari target | `type CheckpointPolicy interface { ShouldCheckpoint(Batch) bool }` | Checkpoint tiap batch aman tapi mahal; tiap-N-batch cepat tapi menambah replay window | Salah policy = duplikasi massal. **Jadikan eksplisit dan terukur** |
| 10 | **Retry / Backoff / Circuit Breaker** | Tangani kegagalan transien | **Tidak ada untuk plugin** (hanya DB connect & lock) | `github.com/cenkalti/backoff/v4`; `internal/resilience` | Retry agresif bisa memperparah beban sumber data | Tanpa klasifikasi error (transien vs permanen) retry akan menyembunyikan bug |
| 11 | **Cancellation / Graceful Shutdown** | Hentikan job tanpa korupsi state | `block.stop(kill=...)`, `_handling_sigterm` | `context.Context`, `signal.NotifyContext`, `os.Process.Signal(syscall.SIGTERM)` | Go jauh lebih rapi di sini; timeout+sinyal terintegrasi | Putus koneksi DB di tengah transaksi meninggalkan staging kotor; butuh cleanup job |
| 12 | **Logging & Tracing** | Log terstruktur, trace per job | `structlog` + `run_id` contextvar + `JobLoggingService` | `log/slog` (JSON handler), `OpenTelemetry` Go SDK | OTel menambah kompleksitas tapi memberi trace antar service | Jangan log nilai kolom data (PII); Meltano juga tidak melakukannya |
| 13 | **Metrics** | Lag, rows/sec, error rate, checkpoint age | Tidak ada padanan langsung (hanya telemetry Snowplow) | `prometheus/client_golang` | Kartinalitas label harus dibatasi | Label per-tabel bisa meledak; batasi dan agregasi |
| 14 | **Connector Registry** | Daftarkan konektor, versi, capability | `base_plugin_factory` + entry points + lockfile | Go plugin (`plugin.Open`) **dihindari**; pakai registry interface + build-tag atau WASM opsional | Registry in-process sederhana; plugin dinamis menyulitkan distribusi | `plugin.Open` rapuh lintas platform/versi Go; Go tidak punya ekosistem packaging seperti pip |
| 15 | **Config & Secret Resolution** | Resolusi berlapis config + env + secret | `SettingsService.get_with_metadata`, 9-lapis merge env | `internal/config`; `caarlos0/env` + Vault/AWS SM | Lapisan eksplisit lebih mudah diaudit | Jangan menyimpan secret di config setara `meltano.yml`; pakai env var / secret manager |

### 8.1 Yang **tidak** boleh disalin mentah-mentah

1. **Komunikasi subprocess + JSON line-delimited sebagai batas utama.** Ini memberi Meltano ekosistem
   Singer, tapi juga menghasilkan batas ukuran pesan (`src/meltano/core/block/future_utils.py:44`), overhead
   proses, dan masking exit code. Untuk Go, gunakan interface in-process + channel.
2. **State sebagai dict JSON bebas.** Meltano hanya memvalidasi keberadaan key `singer_state`
   (`src/meltano/core/state_service.py:139-151`); tidak ada skema, versi, atau tipe. Untuk sistem replikasi,
   state harus bertipe dan berversi.
3. **Locking naif.** File lock berbasis timestamp (`src/meltano/core/state_store/filesystem.py:275-295`) dan
   no-op lock di DB (`src/meltano/core/state_store/db.py:196-222`) tidak memadai untuk multi-worker.
4. **Ketiadaan retry.** Tidak ada padanan apapun untuk dipertahankan di sini.
5. **Checkpoint yang tidak transaksional dengan penulisan data.** Ini akar duplikasi data; desain Go harus
   punya `Commit(state)` di dalam transaksi destination.

### 8.2 Yang **layak diadaptasi**

1. **Pemisahan `partial_state` vs `completed_state`** (`src/meltano/core/state_store/base.py:42-117`) -
   konsep bagus untuk full refresh + incremental bersamaan.
2. **State ID deterministik** dari (environment, source, destination, suffix)
   (`src/meltano/core/block/extract_load.py:894`).
3. **Heartbeat + stale-run failer** (`src/meltano/core/job/job.py:205-230`,
   `src/meltano/core/job/stale_job_failer.py:17`).
4. **Tabel state terpisah dari tabel history** (`src/meltano/core/job_state.py:27`) - history untuk audit,
   tabel `state` untuk lookup cepat.
5. **State backend pluggable via entry points** (`src/meltano/core/state_store/__init__.py:67-102`) - pola
   registry yang bisa dipetakan ke interface Go.
6. **Pencatatan exit code per peran** (`{PluginType: code}`) alih-alih satu exit code generik
   (`src/meltano/core/block/extract_load.py:857`).
7. **Log parser per connector** untuk structured logging dari output eksternal
   (`src/meltano/core/plugin_invoker.py:650`) - berguna bila nanti tetap perlu menjalankan tool eksternal
   (mis. `pg_dump`, LogMiner).

---

**Navigasi:** [Bagian 7](07-diagram-arsitektur.md) - [Daftar Isi](README.md) - [Bagian 9](09-struktur-direktori-go.md)
