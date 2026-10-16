# Replication Control Plane (Go)

Monorepo control plane untuk sistem replikasi database yang memakai Meltano sebagai
execution engine.

Rancangan lengkapnya ada di direktori sebelah, `../meltano-go-reference/`:

| Dokumen | Isi |
|---|---|
| `13-arsitektur-control-plane.md` | arsitektur dan kontrak integrasi |
| `14-api-reference.md` + `14-openapi.yaml` | referensi API + spesifikasi OpenAPI 3.1 |
| `15-menjalankan-meltano.md` | panduan operasional menjalankan Meltano |
| `16-cara-menjalankan-meltano-golang.md` | **runbook end-to-end: API -> worker -> Meltano** |

Untuk mencoba sekarang juga: ikuti **[bagian 16](../meltano-go-reference/16-cara-menjalankan-meltano-golang.md)**
dan impor **[Postman collection](postman/control-plane.postman_collection.json)** (20 request, 6 folder,
ID tersimpan otomatis ke variable).

> **Pindahkan ke repo sendiri.** Direktori ini berada di dalam checkout Meltano
> hanya karena itu satu-satunya lokasi yang bisa ditulis saat dibuat. Untuk
> menjadikannya repo terpisah:
>
> ```bash
> mv controlplane ../controlplane && cd ../controlplane
> git init && git add -A && git commit -m "chore: bootstrap control plane"
> ```
>
> Lalu ganti module path `github.com/example/controlplane` di `go.mod` beserta
> seluruh import di dalamnya.

## Struktur monorepo

```
controlplane/
├── cmd/
│   ├── control-plane/     # binary HTTP API
│   └── worker/            # binary worker eksekusi
├── internal/
│   ├── api/               # router net/http + RFC 7807 problem+json
│   ├── config/            # konfigurasi dari env (prefix CP_)
│   ├── domain/            # tipe inti, tanpa dependency infrastruktur
│   ├── meltano/           # SATU-SATUNYA titik integrasi ke CLI Meltano
│   ├── obs/               # logging log/slog
│   ├── store/             # kontrak persistensi
│   │   └── memory/        # implementasi in-memory
│   └── worker/            # klaim job, heartbeat, eksekusi, reaper lease
├── migrations/            # skema Postgres + query klaim
├── docker-compose.yml
└── env.example
```

Dua binary berbagi satu `internal/`, dan itulah bentuk monorepo di sini. Kalau
nanti perlu memisahkan siklus rilis, pecah menjadi beberapa module dengan
`go.work`; untuk sekarang satu module lebih sederhana.

`internal/meltano` sengaja dijadikan satu-satunya tempat yang tahu detail CLI
Meltano - argv, environment variable, exit code, dan `state_id`. Kalau Meltano
berubah, hanya paket itu yang perlu disentuh.

## Prasyarat

- Go 1.24 atau lebih baru (diuji pada Go 1.26.8)
- Meltano 4.4.1 untuk eksekusi sungguhan
  (lihat `../meltano-go-reference/15-menjalankan-meltano.md`)
- Postgres untuk produksi; belum diperlukan untuk menjalankan API dengan store in-memory

**Tanpa dependency eksternal.** Seluruh kode memakai pustaka standar, sehingga
`go build` dan `go test` berjalan tanpa jaringan. Driver Postgres akan ditambahkan
saat store Postgres diimplementasikan.

## Menjalankan

```bash
cp env.example .env

# API
go run ./cmd/control-plane
# -> {"msg":"control plane mendengarkan","addr":":8080"}

# Worker (terminal lain)
go run ./cmd/worker
```

Postgres untuk pengembangan:

```bash
docker compose up -d postgres
```

Di Windows, arahkan cache Go keluar dari profil bila lingkungan Anda membatasinya:

```bash
export GOCACHE="$TEMP/gocache-cp"
export GOPATH="$TEMP/gopath-cp"
```

## Uji dan build

```bash
go test ./...
go vet ./...
go build ./...

# atau
make test build
```

## Yang sudah berfungsi

| Bagian | Status |
|---|---|
| `internal/meltano` - `StateID`, `SanitizeFilename`, `BuildArgs`, `Classify`, `Adapter.Run` | selesai + unit test |
| `internal/domain` - status run, state machine, validasi | selesai |
| `internal/worker` - klaim, heartbeat, reaper lease, retry otomatis | selesai dengan store in-memory |
| `internal/api` - 18 endpoint (meta, tenant, connection, pipeline, run, event) | selesai dengan store in-memory |
| `migrations/0001_init.sql` - skema + index unik satu run aktif per pipeline | selesai |
| Store Postgres | **belum** |
| Resolver secret (Vault / AWS Secrets Manager) | **belum** - `DenyAllResolver` menolak semua |
| Streaming log (SSE) | **belum** |
| Autentikasi dan tenant dari token | **belum** - masih lewat `tenant_id` di body/query |
| Cakupan OpenAPI doc 14 | sebagian: 18 dari 34 endpoint |

## Cara pakai cepat

```bash
B=http://localhost:8080

# tenant - environment_name menjadi komponen pertama state_id
curl -sX POST $B/v1/tenants -H 'Content-Type: application/json' \
  -d '{"name":"acme","environment_name":"acme"}'

# koneksi - hanya referensi kredensial, tidak pernah nilainya
curl -sX POST $B/v1/connections -H 'Content-Type: application/json' \
  -d '{"tenant_id":"<tenant>","name":"orders-src","kind":"source","driver":"postgres",
       "endpoint":{"host":"db.internal","port":"5432","database":"orders"},
       "username":"replication_ro",
       "secret_ref":"vault://kv/data/replication/prod-orders#password"}'

# pipeline
curl -sX POST $B/v1/pipelines -H 'Content-Type: application/json' \
  -d '{"tenant_id":"<tenant>","name":"orders-sync",
       "extractor":"tap-postgres","loader":"target-postgres",
       "source_connection_id":"<conn>","destination_connection_id":"<conn>",
       "default_timeout_seconds":3600,"max_attempts":3}'

# validasi tanpa eksekusi - mengembalikan resolved_state_id
curl -sX POST $B/v1/pipelines/<pipeline>/validate

# jalankan - Idempotency-Key wajib pada endpoint ini
curl -sX POST $B/v1/pipelines/<pipeline>/runs \
  -H 'Idempotency-Key: 7c1f0e2a-4b3d-4c9e-9f21-1a2b3c4d5e6f' \
  -H 'Content-Type: application/json' -d '{"timeout_seconds":3600}'

# status, event, cancel, retry
curl -s $B/v1/runs/<run>
curl -s $B/v1/runs/<run>/events
curl -sX POST $B/v1/runs/<run>/cancel
curl -sX POST $B/v1/runs/<run>/retry -H 'Idempotency-Key: <uuid>'
```

`409 Conflict` dikembalikan bila pipeline sudah punya run aktif. Ini menggantikan
pengaman advisory Meltano (`JobFinder.latest_running`) dan menjadi alasan field
`force` sengaja tidak ada di API.

## Berikutnya, berurutan

1. **Store Postgres** - implementasi `store.Store` memakai `pgx`, dengan klaim
   atomik `UPDATE ... WHERE id = (SELECT ... FOR UPDATE SKIP LOCKED)` seperti yang
   sudah disiapkan di `migrations/0001_init.sql`. Setelah ini worker bisa
   dijalankan multi-proses.
2. **Resolver secret nyata** - Vault atau AWS Secrets Manager, dipasang ke
   `worker.Secrets`. `DenyAllResolver` sengaja gagal keras supaya password kosong
   tidak pernah terkirim ke connector.
3. **Streaming log (SSE)** - `GET /v1/runs/{id}/logs`. Untuk tail singkat, pakai
   `runs.log_tail` yang sudah disimpan. Untuk streaming langsung, baca file log
   Meltano - lokasinya di luar direktori project, lihat doc 15 bagian 15.6.
4. **Autentikasi dan scope** - ganti `tenant_id` di body/query dengan klaim token,
   mengikuti tabel scope di doc 14 bagian 14.1.2.
5. **Connector native (Go)** untuk Postgres dan MySQL - lihat roadmap doc 10.

## Catatan penting

- **Kredensial tidak pernah masuk control plane.** API hanya menyimpan
  `secret_ref`; worker me-resolve-nya saat spawn. Jangan menambahkan field password
  ke `POST /v1/connections`.
- **Eksekusi selalu `202`.** Tidak ada handler yang menunggu job selesai.
- **Exit code tidak dinormalkan.** Nilai asli disimpan di `runs.exit_code`,
  termasuk kasus Meltano menormalkannya sendiri (doc 6 bagian 6.7).
- **Meltano harus sudah ter-provision per tenant** sebelum job dijalankan. Worker
  tidak pernah memanggil `add` atau `install`.
- **`state_id` dihitung control plane**, bukan dibaca dari Meltano. Formula dan
  validasi charset-nya ada di `internal/meltano/stateid.go`.

## Status verifikasi

`go build ./...`, `go vet ./...`, dan `go test ./...` dijalankan pada Go 1.26.8
(Windows) dan semuanya lolos. Unit test mencakup `StateID`, `SanitizeFilename`,
`BuildArgs`, dan `Classify` - termasuk kasus nyata `dev:tap-gitlab-to-target-jsonl`
yang menjadi `devtap-gitlab-to-target-jsonl`.

Yang **belum** terverifikasi: `Adapter.Run` belum diuji end-to-end terhadap Meltano
sungguhan dari kode ini. Kontrak argv dan environment yang dipakai berasal dari uji
manual yang tercatat di doc 15 bagian 15.6.
