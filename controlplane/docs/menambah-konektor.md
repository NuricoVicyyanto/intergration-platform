# Menambah Driver / Konektor

Dokumen ini menjawab satu pertanyaan: **kalau saya mau menambah konektor baru (misalnya Snowflake,
atau API internal), apa saja yang harus diubah?**

Ada tiga jalur yang berbeda, dan urutannya penting - jalur paling murah dipakai lebih dulu:

| Jalur | Kapan dipakai | Perubahan kode |
|---|---|---|
| **0. Plugin Meltano baru** | Konektor sudah ada sebagai Singer tap/target (mayoritas kasus) | tidak ada |
| **1. Label driver baru** | Perlu nilai `driver` baru di `POST /v1/connections` | 1 konstanta + 1 file migrasi |
| **2. Pemetaan env per plugin** | Nama setting plugin tidak cocok konvensi generik | 1 tipe + 1 titik wiring |
| **3. Connector native Go** | Tidak mau lewat Meltano sama sekali | paket baru + integrasi worker (belum ada) |

---

## 0. Yang perlu dipahami dulu: driver bukan konektor

`connections.driver` hanyalah **label + validasi skema**. Yang benar-benar memindahkan data adalah plugin
Meltano yang dieksekusi worker:

```
POST /v1/pipelines  { extractor: "tap-postgres", loader: "target-postgres" }
        |
        v
worker: meltano --environment=<env> run tap-postgres target-postgres --run-id=... --no-install
```

Jadi menambah sumber data baru seringkali **tidak butuh perubahan kode sama sekali**. Yang dibutuhkan hanya:

1. Plugin terpasang di project tenant (`--no-install` selalu dikirim; worker tidak pernah `add`/`install`).
2. `extractor`/`loader` pada pipeline sama persis dengan nama plugin di `meltano.yml` tenant itu.
3. `secret_ref` pada connection bisa di-resolve worker.

```bash
B=http://127.0.0.1:8080

# 1) sekali per tenant, di direktori project Meltano tenant:
#    meltano add extractor tap-postgres && meltano add loader target-postgres && meltano install

# 2) daftarkan koneksi (hanya referensi kredensial, bukan nilainya)
curl -sX POST $B/v1/connections -H 'Content-Type: application/json' -d '{
  "tenant_id":"<tenantId>","name":"orders-src","kind":"source","driver":"postgres",
  "endpoint":{"host":"db.internal","port":"5432","database":"orders"},
  "username":"replication_ro","secret_ref":"orders-src-password"}'

# 3) pipeline memakai plugin apa pun yang sudah terpasang
curl -sX POST $B/v1/pipelines -H 'Content-Type: application/json' -d '{
  "tenant_id":"<tenantId>","name":"orders-sync",
  "extractor":"tap-postgres","loader":"target-postgres",
  "source_connection_id":"<connSrc>","destination_connection_id":"<connDst>",
  "default_timeout_seconds":3600,"max_attempts":3}'
```

Kalau plugin butuh setting yang tidak dipetakan konvensi generik (lihat jalur 2), atau kalau belum ada plugin
Singer-nya sama sekali (jalur 3), baru turun ke jalur berikutnya.

---

## 1. Menambah label driver baru

Contoh: menambah `mongodb`.

**a. Konstanta domain** - `internal/domain/domain.go`:

```go
const (
	DriverPostgres Driver = "postgres"
	DriverMySQL    Driver = "mysql"
	DriverMSSQL    Driver = "mssql"
	DriverOracle   Driver = "oracle"
	DriverMongoDB  Driver = "mongodb" // baru
)
```

**b. Migrasi baru - JANGAN mengedit `0001_init.sql`.**

`0001_init.sql` sudah tercatat di tabel `schema_migrations` (dan `CREATE TABLE IF NOT EXISTS` tidak mengubah
tabel yang sudah ada), jadi mengeditnya tidak berpengaruh ke database yang sudah jalan. Tambahkan file
`migrations/0002_driver_mongodb.sql`:

```sql
-- Nama constraint dicek dulu: SELECT conname FROM pg_constraint
--   WHERE conrelid = 'connections'::regclass;
ALTER TABLE connections DROP CONSTRAINT IF EXISTS connections_driver_check;
ALTER TABLE connections ADD CONSTRAINT connections_driver_check
  CHECK (driver IN ('postgres', 'mysql', 'mssql', 'oracle', 'mongodb'));
```

File baru langsung dieksekusi oleh runner migrasi (`store/postgres.Migrate`) saat API/worker start, karena
isinya di-embed lewat `migrations/embed.go`. Untuk menjalankan manual tanpa restart:

```bash
cd controlplane
psql "$CP_DATABASE_URL" -f migrations/0002_driver_mongodb.sql
```

Catatan untuk instalasi Docker: `docker-entrypoint-initdb.d` hanya menjalankan migrasi pada volume kosong.
Untuk volume yang sudah ada, jalankan file `0002_*.sql` manual atau biarkan runner migrasi di API yang
mengerjakannya.

**c. (Opsional) validasi di API.** Sekarang `driver` tidak divalidasi di handler, jadi nilai ngawur baru
ditolak oleh CHECK constraint database dan muncul sebagai `422 unprocessable`. Kalau ingin pesan error yang
lebih baik, tambahkan validasi di `internal/api/server.go:handleCreateConnection`, sejajar dengan validasi
`kind`:

```go
switch domain.Driver(in.Driver) {
case domain.DriverPostgres, domain.DriverMySQL, domain.DriverMSSQL, domain.DriverOracle, domain.DriverMongoDB:
default:
	writeError(w, r, errors.Join(domain.ErrInvalid, errors.New("driver tidak dikenal")))
	return
}
```

**d. Samakan perilaku store in-memory.** `internal/store/memory/memory.go` tidak memvalidasi driver sama
sekali, jadi tanpa perubahan ini demo `memory://` akan menerima driver yang ditolak Postgres. Tambahkan
validasi yang sama di `CreateConnection` kalau Anda memakai mode itu untuk uji.

**e. Uji:**

```bash
go build ./... && go vet ./... && go test ./...
curl -sX POST $B/v1/connections -H 'Content-Type: application/json' \
  -d '{"tenant_id":"<t>","name":"mongo-src","kind":"source","driver":"mongodb",
       "endpoint":{"host":"mongo.internal"},"secret_ref":"mongo-src-password"}'
# driver di luar daftar -> 422
```

---

## 2. Memetakan setting plugin (EnvBuilder)

Worker menerjemahkan satu `connection` menjadi environment variable untuk connector lewat
interface berikut (`internal/worker/worker.go`):

```go
type EnvBuilder interface {
	Build(kind domain.ConnectionKind, conn domain.Connection, password string) (map[string]string, error)
}
```

Default-nya `ConventionEnvBuilder`, yang memetakan `endpoint.host/port/database` + `username` + password ke
konvensi generik Meltano dan prefix plugin (`TAP_<DRIVER>_` / `TARGET_<DRIVER>_`):

```
MELTANO_EXTRACT_HOST / _PORT / _DATABASE / _USER / _PASSWORD   (kind=source)
MELTANO_LOAD_HOST    / _PORT / _DATABASE / _USER / _PASSWORD   (kind=destination)
```

Untuk PostgreSQL, builder juga menyusun `sqlalchemy_url` karena `tap-postgres` mewajibkannya.
Credential di-escape dengan `net/url`, `sslmode` diteruskan, dan sisi target memakai
`postgresql+psycopg://`. Default target schema adalah `endpoint.schema` atau `public`, dengan
`load_method=upsert` supaya run berulang tidak menambah duplikat. `endpoint.schemas` dapat berisi
array schema sumber.

Plugin nyata sering memakai nama lain (`DB_HOST`, `SCHEMA`, `SID`, ...). Buat implementasi khusus lalu
dispatch per driver:

```go
// internal/worker/envbyDriver.go
type DriverEnvBuilder struct {
	ByDriver map[domain.Driver]EnvBuilder
	Fallback EnvBuilder // ConventionEnvBuilder
}

func (b DriverEnvBuilder) Build(kind domain.ConnectionKind, conn domain.Connection, password string) (map[string]string, error) {
	if impl, ok := b.ByDriver[conn.Driver]; ok {
		return impl.Build(kind, conn, password)
	}
	if b.Fallback == nil {
		return ConventionEnvBuilder{}.Build(kind, conn, password)
	}
	return b.Fallback.Build(kind, conn, password)
}
```

Implementasi per driver cukup mengembalikan map nama setting -> nilai, dengan prefix sesuai arah aliran:

```go
type OracleEnvBuilder struct{}

func (OracleEnvBuilder) Build(kind domain.ConnectionKind, c domain.Connection, password string) (map[string]string, error) {
	prefix := "MELTANO_EXTRACT_"
	if kind == domain.KindDestination {
		prefix = "MELTANO_LOAD_"
	}
	get := func(k string) string { s, _ := c.Endpoint[k].(string); return s }
	return map[string]string{
		prefix + "HOST":     get("host"),
		prefix + "PORT":     get("port"),
		prefix + "SERVICE":  get("service"), // contoh setting khusus
		prefix + "USER":     c.Username,
		prefix + "PASSWORD": password,
	}, nil
}
```

Wiring ada di **dua tempat** - jangan lupa yang kedua:

- `cmd/worker/main.go` -> `worker.Worker{ Env: ... }`
- `cmd/control-plane/main.go`, blok `CP_EMBEDDED_WORKER=true` -> `worker.Worker{ Env: ... }`

```go
Env: worker.DriverEnvBuilder{ByDriver: map[domain.Driver]worker.EnvBuilder{
	domain.DriverOracle: worker.OracleEnvBuilder{},
}},
```

Nama environment variable diverifikasi ke plugin, bukan ke dokumentasi umum:

```bash
cd <project tenant>
meltano config list tap-postgres --all  # daftar setting yang dikenali plugin
```

Aturan Meltano: setting `foo_bar` pada plugin `x` dibaca dari `MELTANO_EXTRACT_FOO_BAR`. Cara paling aman
adalah menulis nama lengkapnya di `EnvBuilder` seperti contoh di atas.

**Kredensial tetap keluar dari `secret_ref`, bukan dari request API.** Nilai password di-resolve worker
lewat `Worker.Secrets` (`internal/worker/worker.go`). Sekarang `cmd/worker` memakai `EnvResolver`, yaitu
membaca `CP_SECRET_<REF>` dari environment proses worker (non-alfanumerik menjadi `_`, uppercase). Untuk
produksi ganti dengan Vault / Secrets Manager dan jangan menambah field password ke `POST /v1/connections`.

---

## 3. Connector native Go (belum ada)

Roadmap tahap ini ada di [`../../meltano-go-reference/10-roadmap-implementasi.md`](../../meltano-go-reference/10-roadmap-implementasi.md).
Ringkasnya, alur berubah dari "worker spawn Meltano" menjadi "worker menjalankan connector Go", sehingga
jawabannya bukan lagi `EnvBuilder` melainkan paket connector:

```
internal/connector/<driver>/    # baca source / tulis destination
internal/worker/                # pilih connector per pipeline, bukan tap/target Meltano
```

Yang harus direplikasi dari kontrak Meltano kalau jalur ini diambil (lihat
[`../../meltano-go-reference/05-state-checkpoint-recovery.md`](../../meltano-go-reference/05-state-checkpoint-recovery.md)):
penulisan state per stream, checkpoint non-transaksional (konsekuensinya at-least-once), retry, dan
klasifikasi exit code. Ini pekerjaan besar - pakai jalur 0-2 selama masih bisa.

---

## Hal yang tidak boleh dilakukan

- **Jangan menyimpan nilai kredensial** di tabel `connections` - hanya `secret_ref`.
- **Jangan mengedit `0001_init.sql`** pada database yang sudah jalan; buat `0002_*.sql`.
- **Jangan mengandalkan `meltano install` saat run** - adapter selalu mengirim `--no-install`; plugin harus
  terpasang lebih dulu di project tenant.
- **Jangan mengganti nama `extractor`/`loader` tanpa sadar**: `state_id` dibentuk dari
  `{environment}:{extractor}-to-{loader}[:{suffix}]` (`internal/meltano/stateid.go`), jadi nama baru berarti
  state baru dan sync berikutnya mulai dari nol.
- **Jangan menambah endpoint `force`** untuk menembus pengaman "satu run aktif per pipeline"; pengaman itu
  disengaja (lihat `meltano-go-reference/14-api-reference.md` bagian 14.8).

---

## Checklist singkat

| Langkah | Berkas |
|---|---|
| Label driver baru | `internal/domain/domain.go` + `migrations/0002_*.sql` |
| Pesan validasi driver | `internal/api/server.go` |
| Perilaku store in-memory | `internal/store/memory/memory.go` |
| Pemetaan setting plugin | `internal/worker/` + wiring di `cmd/worker/main.go` dan `cmd/control-plane/main.go` |
| Kredensial | `internal/worker/worker.go` (`SecretResolver`), `CP_SECRET_*` untuk dev |
| Verifikasi | `go build ./... && go vet ./... && go test ./...`, lalu alur API: connection -> pipeline -> validate -> run |

Cek hasil di database setelah satu run:

```sql
SELECT id, status, exit_code, left(coalesce(error, ''), 80) AS error, ended_at
  FROM runs ORDER BY created_at DESC LIMIT 5;
```
