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
| [`docs/menambah-konektor.md`](docs/menambah-konektor.md) | cara menambah driver / konektor baru |
| [`docs/source-selection-dan-row-filter.md`](docs/source-selection-dan-row-filter.md) | cara memilih tabel/stream dan memfilter record pada pipeline |
| [`docs/copy-postgres-schema-gateway-log.md`](docs/copy-postgres-schema-gateway-log.md) | contoh copy PostgreSQL dari schema `gateway_log` |
| [`docs/connection-types.md`](docs/connection-types.md) | referensi field dan contoh semua tipe connection |

Untuk mencoba sekarang juga: ikuti **[bagian 16](../meltano-go-reference/16-cara-menjalankan-meltano-golang.md)**
dan impor **[Postman collection](postman/control-plane.postman_collection.json)** (20 request, 6 folder,
ID tersimpan otomatis ke variable).

## Docker: Python + Go + Svelte + database eksternal

Stack Docker menjalankan API Go, worker Go + Meltano/Python, provisioning tenant,
dan UI Svelte dalam satu perintah. PostgreSQL tidak ikut dijalankan oleh Compose;
kedua DSN harus menunjuk database eksternal yang sudah dibuat.

```bash
# dari root repository
cp docker.env.example .env.docker
# isi CP_DATABASE_URL dan CP_SYSTEM_DB_URI di .env.docker
docker compose up --build
```

UI: `http://localhost:3000`
API: `http://localhost:8080`

Database control plane dan system database Meltano harus berbeda karena keduanya
memakai nama tabel yang sama. Jika PostgreSQL berada di host Docker Desktop,
gunakan `host.docker.internal` pada DSN. Panduan lengkap ada di
[`docker/README.md`](docker/README.md).

Runbook terperinci:

- [`docs/docker-local-step-by-step.md`](docs/docker-local-step-by-step.md) -
  instalasi dan verifikasi local dari nol
- [`docs/docker-server-deployment.md`](docs/docker-server-deployment.md) -
  deployment server, akses aman, backup, update, dan rollback
- [`docs/docker-oracle-linux-server.md`](docs/docker-oracle-linux-server.md) -
  instalasi khusus Oracle Linux 8/9

Script operasional:

- [`deploy/deploy.sh`](deploy/deploy.sh) - build, start, menunggu provisioning,
  dan health check
- [`deploy/check.sh`](deploy/check.sh) - verifikasi API, worker, UI, dan database
- [`deploy/backup.sh`](deploy/backup.sh) - backup dua database dan dua volume
- [`deploy/Caddyfile.example`](deploy/Caddyfile.example) - reverse proxy HTTPS
  dengan Basic Auth

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
│   ├── provision/         # provisioning project + plugin tenant
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
├── docker/                # script Nginx dan bootstrap tenant
├── docker-compose.yml     # include stack Docker di root repository
└── env.example
```

Dua binary berbagi satu `internal/`, dan itulah bentuk monorepo di sini. Kalau
nanti perlu memisahkan siklus rilis, pecah menjadi beberapa module dengan
`go.work`; untuk sekarang satu module lebih sederhana.

`internal/meltano` sengaja dijadikan satu-satunya tempat yang tahu detail CLI
Meltano - argv, environment variable, exit code, dan `state_id`. Kalau Meltano
berubah, hanya paket itu yang perlu disentuh.

## Prasyarat pengembangan lokal

- Go 1.24 atau lebih baru (diuji pada Go 1.26.8)
- Meltano 4.4.1 untuk eksekusi sungguhan
  (lihat `../meltano-go-reference/15-menjalankan-meltano.md`)
- Postgres untuk produksi; belum diperlukan untuk menjalankan API dengan store in-memory

## Menjalankan tanpa Docker

```bash
cp env.example .env

# API
go run ./cmd/control-plane
# -> {"msg":"control plane mendengarkan","addr":":8080"}

# Worker (terminal lain)
go run ./cmd/worker
```

UI Svelte tersedia di `ui/` dan memakai proxy Vite ke API pada `http://localhost:8080`:

```bash
cd ui
npm install
npm run dev
```

Buka `http://localhost:5173`. Jalankan API lebih dulu dengan `memory://` untuk demo lokal tanpa
Postgres (`CP_DATABASE_URL=memory://`, atau sesuaikan `.env`). UI memakai endpoint v1 yang sama
dengan Postman collection: tenant, connections, connectors, pipelines, runs, dan health checks.
Workspace, connection, dan pipeline dapat dibuat, diedit, dan dihapus langsung dari UI. Run tetap
immutable karena merupakan riwayat eksekusi. Halaman memakai deep link `#overview`, `#pipelines`,
`#connections`, dan `#connectors`. Logo PostgreSQL, MySQL, SQL Server, dan Oracle berasal dari aset
brand Devicon.

Jika API tidak berjalan di port `8080`, arahkan proxy Vite saat start:

```bash
CP_API_PROXY=http://127.0.0.1:18082 npm run dev
```

Saat menambahkan koneksi PostgreSQL dengan password dari UI, control plane melakukan ping dan
autentikasi terlebih dahulu. Edit koneksi boleh tidak mengirim password untuk mempertahankan secret
yang sudah ada.

Untuk demo lokal, UI menyediakan input `Password`. Aktifkan secara eksplisit:

```bash
CP_DATABASE_URL=memory:// CP_ALLOW_PLAINTEXT_SECRETS=true CP_EMBEDDED_WORKER=true go run ./cmd/control-plane
```

Mode ini menyimpan password di store control-plane dan **tidak boleh digunakan di production**.
Default tetap `false`; deployment production harus menggunakan `secret_ref` dan resolver secret manager.

Untuk pengembangan lokal dengan PostgreSQL, sediakan database eksternal dan isi
`CP_DATABASE_URL` di `.env`. File Compose lama yang mengelola PostgreSQL lokal
sudah diganti dengan include stack Docker di root repository.

Jika ingin tetap memakai database lokal tanpa stack aplikasi:

```bash
docker run --rm -d --name controlplane-postgres \
  -e POSTGRES_USER=controlplane \
  -e POSTGRES_PASSWORD=controlplane \
  -e POSTGRES_DB=controlplane \
  -p 5432:5432 postgres:16-alpine
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
| `internal/api` - 27 endpoint (meta, tenant, connection, pipeline, run, event, execution log) | selesai dengan store in-memory dan Postgres |
| Pipeline source selection (`streams` + Singer `row_filter`) | selesai untuk tap-postgres/mysql/mssql/mongodb; tap-oracle mendukung pemilihan tabel saja |
| Pipeline performance tuning | batch size, validasi record, dan load strategy per pipeline untuk `target-postgres` |
| `migrations/0001_init.sql` - skema + index unik satu run aktif per pipeline | selesai |
| Store Postgres | selesai, termasuk klaim job atomik dan migrasi |
| MongoDB connector | `tap-mongodb` (meltanolabs), `target-mongodb` (hotgluexyz), driver `mongodb` |
| File/API connectors | `tap-rest-api-msdk`, `tap-csv`, `tap-google-sheets`, `target-s3`, `target-duckdb` |
| Resolver secret | `EnvResolver` untuk development; Vault/AWS Secrets Manager belum tersedia |
| Execution log dan progres (`GET /v1/runs/{id}/logs`) | selesai; UI melakukan polling saat run aktif |
| Stop run dari Overview | queued dibatalkan langsung; running dihentikan melalui context cancellation worker |
| Streaming log (SSE) | **belum** |
| Autentikasi dan tenant dari token | **belum** - masih lewat `tenant_id` di body/query |
| Cakupan OpenAPI doc 14 | sebagian: 27 dari 34 endpoint |

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
       "source_filter":{"streams":["public-orders"],"row_filter":"status == \"active\""},
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

# edit parsial
curl -sX PATCH $B/v1/connections/<conn> -H 'Content-Type: application/json' \
  -d '{"name":"orders-primary","username":"replication_reader"}'

curl -sX PATCH $B/v1/pipelines/<pipeline> -H 'Content-Type: application/json' \
  -d '{"name":"orders-to-warehouse","enabled":false}'

# hapus resource
curl -sX DELETE $B/v1/pipelines/<pipeline>
curl -sX DELETE $B/v1/connections/<conn>
curl -sX DELETE $B/v1/tenants/<tenant>
```

Contoh koneksi MongoDB:

```json
{
  "tenant_id": "<tenant>",
  "name": "mongo-source",
  "kind": "source",
  "driver": "mongodb",
  "endpoint": {
    "host": "mongo.internal",
    "port": "27017",
    "database": "app",
    "auth_source": "admin",
    "sslmode": "require"
  },
  "username": "mongo_reader",
  "secret_ref": "vault://kv/data/mongo#password"
}
```

`source_filter.streams` menerima nama stream Singer yang tepat atau pola glob seperti
`sales-*`. Kosongkan untuk memindahkan semua stream. `source_filter.row_filter` adalah
ekspresi stream-map Singer, misalnya `status == "active" and amount >= 100`; filter ini
diterapkan saat record keluar dari extractor, bukan sebagai push-down SQL ke database.
`tap-oracle` legacy hanya mendukung pemilihan tabel, belum filter baris.

Pipeline PostgreSQL juga menerima `transfer_options`:

```json
{
  "batch_size_rows": 20000,
  "validate_records": false,
  "load_method": "append-only"
}
```

- `batch_size_rows` menurunkan overhead commit tetapi memakai RAM lebih besar.
- `validate_records: false` menghilangkan validasi schema per record; gunakan hanya
  untuk source yang sudah dipercaya.
- `load_method` dapat mengoverride strategi dari destination connection.
- Opsi ini hanya berlaku pada pipeline tersebut dan tidak mengubah pipeline lain.

`409 Conflict` dikembalikan bila pipeline sudah punya run aktif. Ini menggantikan
pengaman advisory Meltano (`JobFinder.latest_running`) dan menjadi alasan field
`force` sengaja tidak ada di API.

Penghapusan connection juga menghasilkan `409` selama pipeline masih memakainya.
Workspace/pipeline tidak dapat dihapus selama run masih aktif. Menghapus tenant dari
control plane tidak menghapus direktori project Meltano di disk.

### PostgreSQL: server A ke server B

Contoh ini memakai Postgres di kedua sisi. Provisioning plugin dilakukan sekali
per tenant, sebelum pipeline dijalankan:

```bash
# project berada di CP_TENANTS_ROOT/acme
go run ./cmd/provision -tenant acme \
  -extractor tap-postgres -loader target-postgres
```

Lalu daftarkan dua koneksi dan pipeline. `schema` opsional dan default-nya
`public`; `sslmode` juga diteruskan ke connector:

```bash
curl -sX POST $B/v1/connections -H 'Content-Type: application/json' -d '{
  "tenant_id":"<tenant>","name":"server-a","kind":"source","driver":"postgres",
  "endpoint":{"host":"server-a.internal","port":"5432","database":"app",
              "schema":"public","sslmode":"require"},
  "username":"replication_ro","secret_ref":"server-a-password"}'

curl -sX POST $B/v1/connections -H 'Content-Type: application/json' -d '{
  "tenant_id":"<tenant>","name":"server-b","kind":"destination","driver":"postgres",
  "endpoint":{"host":"server-b.internal","port":"5432","database":"app_copy",
              "schema":"public","sslmode":"require"},
  "username":"replication_writer","secret_ref":"server-b-password"}'
```

Worker menyusun `sqlalchemy_url` dengan escaping aman, memakai
`postgresql://` untuk tap dan `postgresql+psycopg://` untuk target, serta
default `load_method=upsert`. Dengan begitu run berulang memperbarui baris
berdasarkan primary key dan tidak sekadar menambah duplikat. Tabel sumber
sebaiknya memiliki primary key. Untuk mengubah perilaku load, kirim
`options.load_method` pada koneksi tujuan (`append-only`, `upsert`, atau
`overwrite`).

Ini adalah replikasi tabel secara **logis** melalui Singer/Meltano, bukan clone
fisik database. Primary key, index, trigger, sequence, view, function, dan data
yang tidak dipilih tidak otomatis dipindahkan. Untuk clone/restore persis,
gunakan `pg_dump`/`pg_restore`; gunakan pipeline ini untuk sinkronisasi tabel
yang berulang.

## Berikutnya, berurutan

1. **Resolver secret nyata** - Vault atau AWS Secrets Manager, dipasang ke
   `worker.Secrets`. `DenyAllResolver` sengaja gagal keras supaya password kosong
   tidak pernah terkirim ke connector.
2. **Streaming log (SSE)** - `GET /v1/runs/{id}/logs`. Untuk tail singkat, pakai
   `runs.log_tail` yang sudah disimpan. Untuk streaming langsung, baca file log
   Meltano - lokasinya di luar direktori project, lihat doc 15 bagian 15.6.
3. **Autentikasi dan scope** - ganti `tenant_id` di body/query dengan klaim token,
   mengikuti tabel scope di doc 14 bagian 14.1.2.
4. **Connector native (Go)** untuk Postgres dan MySQL - lihat roadmap doc 10.

## Catatan penting

- **Kredensial tidak pernah masuk control plane.** API hanya menyimpan
  `secret_ref`; worker me-resolve-nya saat spawn. Jangan menambahkan field password
  ke `POST /v1/connections`.
- **Eksekusi selalu `202`.** Tidak ada handler yang menunggu job selesai.
- **Exit code tidak dinormalkan.** Nilai asli disimpan di `runs.exit_code`,
  termasuk kasus Meltano menormalkannya sendiri (doc 6 bagian 6.7).
- **Meltano harus sudah ter-provision per tenant** sebelum job dijalankan. Worker
  tidak pernah memanggil `add` atau `install`. Gunakan `cmd/provision` untuk
  langkah satu kali ini.
- **`state_id` dihitung control plane**, bukan dibaca dari Meltano. Formula dan
  validasi charset-nya ada di `internal/meltano/stateid.go`.

## Status verifikasi

`go test ./...` dan build dijalankan pada Go 1.26.8 (Windows). Unit test mencakup
pemetaan environment PostgreSQL, validasi source/destination, provisioning,
`StateID`, `SanitizeFilename`, `BuildArgs`, dan `Classify`.

Alur API -> worker -> Meltano pernah diuji end-to-end dengan dua container
PostgreSQL 16 lokal: tabel `public.orders` berisi tiga baris disalin ke server B;
setelah satu baris diubah dan satu baris ditambah di A, run berikutnya menghasilkan
tepat empat baris di B tanpa duplikasi. Ini membuktikan replikasi tabel PostgreSQL
dan update berulang untuk data uji tersebut.
