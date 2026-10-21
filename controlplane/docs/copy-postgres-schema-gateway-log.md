# Copy PostgreSQL Schema `gateway_log`

Panduan singkat untuk memindahkan data dari schema PostgreSQL `gateway_log`,
bukan schema default `public`.

## 1. Pastikan user database dapat membaca schema

User PostgreSQL pada source connection harus memiliki akses ke schema dan tabel
`gateway_log`.

Contoh pengecekan:

```sql
SELECT table_schema, table_name
FROM information_schema.tables
WHERE table_schema = 'gateway_log'
ORDER BY table_name;
```

Jika hasilnya kosong, periksa nama schema, hak akses user, atau database yang
dipilih.

## 2. Buat source connection PostgreSQL

Di UI:

1. Buka **Connections**.
2. Klik **Add connection**.
3. Pilih **Role → Source**.
4. Pilih **Database engine → PostgreSQL**.
5. Isi host, port, dan database.
6. Isi **Schema** dengan:

   ```text
   gateway_log
   ```

7. Isi username dan secret reference/password.
8. Simpan connection.

Bagian pentingnya adalah field **Schema** pada source connection. Nilai tersebut
diteruskan sebagai `filter_schemas` extractor, sehingga `tap-postgres` hanya
melakukan discovery dan sync pada schema `gateway_log`.

Untuk connection PostgreSQL yang sudah ada, buka edit connection dan ubah
**Schema** dari `public` menjadi `gateway_log`.

## 3. Atur destination schema

Buat atau edit destination connection PostgreSQL. Jika data ingin ditulis ke
schema `public`, isi **Schema** destination dengan:

```text
public
```

Source schema dan destination schema adalah konfigurasi terpisah:

- **Source connection schema** menentukan schema yang dibaca.
- **Destination connection schema** menentukan schema tujuan.

## 4. Buat pipeline

Di form **Create pipeline**:

- **Source connection:** pilih koneksi dengan schema `gateway_log`.
- **Destination connection:** pilih koneksi tujuan, misalnya schema `public`.
- **Extractor:** `tap-postgres`.
- **Loader:** `target-postgres`.

Untuk memindahkan seluruh tabel dalam schema `gateway_log`, kosongkan
**Tables or streams**.

Untuk memilih tabel tertentu, isi nama stream dengan format
`gateway_log-<nama_tabel>`.

## 5. Pilih tabel tertentu

Contoh tabel source:

```text
gateway_log.requests
gateway_log.upstream_errors
gateway_log.audit_events
```

Isi **Tables or streams**:

```text
gateway_log-requests
gateway_log-upstream_errors
gateway_log-audit_events
```

Untuk memilih semua stream dari schema tersebut:

```text
gateway_log-*
```

Nama stream menggunakan tanda hubung (`-`), bukan titik (`.`).

Jika nama stream tidak sesuai, jalankan pipeline sekali tanpa table selection,
lalu lihat log discovery pada **Overview → Execution trace**. Cari pesan:

```text
Beginning full_table sync of 'gateway_log-requests'...
```

Gunakan nilai di dalam tanda kutip.

## 6. Filter record tertentu

Contoh hanya mengambil request dengan status `error`:

**Tables or streams**

```text
gateway_log-requests
```

**Row filter**

```text
status == "error"
```

Contoh lain:

```text
status == "error" and response_time_ms >= 1000
```

Row filter menggunakan ekspresi Singer, bukan query SQL. Filter diterapkan
setelah record diekstrak dan sebelum masuk ke target.

## 7. Contoh API

Source connection:

```json
{
  "tenant_id": "<tenant>",
  "name": "gateway-log-source",
  "kind": "source",
  "driver": "postgres",
  "endpoint": {
    "host": "postgres.internal",
    "port": "5432",
    "database": "gateway",
    "schema": "gateway_log",
    "sslmode": "require"
  },
  "username": "gateway_reader",
  "secret_ref": "vault://kv/data/gateway-postgres#password"
}
```

Pipeline:

```json
{
  "tenant_id": "<tenant>",
  "name": "gateway-log-sync",
  "extractor": "tap-postgres",
  "loader": "target-postgres",
  "source_connection_id": "<gateway-log-source-id>",
  "destination_connection_id": "<destination-id>",
  "source_filter": {
    "streams": [
      "gateway_log-requests",
      "gateway_log-upstream_errors"
    ],
    "row_filter": "status == \"error\""
  },
  "default_timeout_seconds": 3600,
  "max_attempts": 3
}
```

## 8. Verifikasi

Jalankan pipeline, lalu buka **Overview → Execution trace**.

Periksa:

- stream yang muncul harus diawali `gateway_log-`;
- jumlah **Records extracted**;
- jumlah **Records loaded**;
- log discovery dan query dari `tap-postgres`.

Jika masih muncul stream dari schema lain, periksa kembali field **Schema** pada
source connection. Pastikan nilainya tepat:

```text
gateway_log
```

dan bukan `public`, kosong, atau `"gateway log"`.
