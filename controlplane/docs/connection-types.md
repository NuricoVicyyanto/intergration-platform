# Referensi Semua Tipe Connection

Dokumen ini menjelaskan field untuk setiap tipe connection pada halaman
**Connections → Add connection** dan endpoint `POST /v1/connections`.

## Kontrak umum

```json
{
  "tenant_id": "<tenant-id>",
  "name": "connection-name",
  "kind": "source",
  "driver": "postgres",
  "endpoint": {},
  "username": "",
  "secret_ref": "",
  "options": {}
}
```

| Field | Keterangan |
|---|---|
| `tenant_id` | Workspace pemilik connection |
| `name` | Nama unik connection dalam workspace |
| `kind` | `source` atau `destination` |
| `driver` | Tipe connector |
| `endpoint` | Konfigurasi koneksi; bentuknya berbeda per driver |
| `username` | User database, access key ID, atau username autentikasi |
| `secret_ref` | Referensi secret yang di-resolve worker |
| `password` | Hanya untuk development jika `CP_ALLOW_PLAINTEXT_SECRETS=true` |
| `options` | Opsi plugin tambahan |

Nilai password, token, dan secret tidak pernah dikembalikan oleh API.

## Cara kerja `secret_ref`

Worker development membentuk environment variable:

```text
CP_SECRET_<SECRET_REF>
```

Karakter selain huruf dan angka diubah menjadi `_`, lalu di-uppercase.

Contoh:

```text
secret_ref: google-oauth
environment: CP_SECRET_GOOGLE_OAUTH='{"client_id":"..."}'
```

Untuk production sebaiknya gunakan Vault atau secret manager, bukan environment
worker.

## Ringkasan driver

| Driver | Role | Credential database | Tipe konfigurasi |
|---|---|---|---|
| `postgres` | source/destination | Wajib | Host, database |
| `mysql` | source/destination | Wajib | Host, database |
| `mssql` | source/destination | Wajib | Host, database |
| `oracle` | source/destination | Wajib | Host, service/SID |
| `mongodb` | source/destination | Wajib | Host, database, auth source |
| `rest` | source | Tergantung auth | API URL |
| `csv` | source | Tidak perlu | Path file/directory |
| `google-sheets` | source | OAuth JSON | Spreadsheet ID |
| `s3` | destination | Opsional | Bucket dan region |
| `duckdb` | destination | Tidak perlu | Path file DuckDB |

---

## 1. PostgreSQL

Driver:

```text
postgres
```

Field:

| Field | Wajib | Contoh |
|---|---|---|
| Host | Ya | `postgres.internal` |
| Port | Ya | `5432` |
| Database | Ya | `gateway` |
| Schema | Ya | `public` atau `gateway_log` |
| SSL mode | Ya | `require` |
| Username | Ya | `gateway_reader` |
| Secret reference | Ya | `gateway-postgres-password` |

Source:

- `schema` menentukan schema yang dibaca melalui `filter_schemas`.
- Kosongkan source schema hanya jika memang ingin semua schema.

Destination:

- `schema` menentukan target schema.
- `options.load_method`: `upsert`, `overwrite`, atau `append-only`.
- Default worker: `upsert`.

Contoh:

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
  "secret_ref": "gateway-postgres-password"
}
```

---

## 2. MySQL

Driver:

```text
mysql
```

Field:

| Field | Wajib | Contoh |
|---|---|---|
| Host | Ya | `mysql.internal` |
| Port | Ya | `3306` |
| Database | Ya | `orders` |
| SSL mode | Ya | `prefer` |
| Username | Ya | `replication_ro` |
| Secret reference | Ya | `mysql-orders-password` |

Destination `options.load_method` dapat diisi `upsert`, `overwrite`, atau
`append-only`.

Contoh:

```json
{
  "tenant_id": "<tenant>",
  "name": "orders-mysql",
  "kind": "source",
  "driver": "mysql",
  "endpoint": {
    "host": "mysql.internal",
    "port": "3306",
    "database": "orders",
    "sslmode": "prefer"
  },
  "username": "replication_ro",
  "secret_ref": "mysql-orders-password"
}
```

---

## 3. Microsoft SQL Server

Driver:

```text
mssql
```

Field:

| Field | Wajib | Contoh |
|---|---|---|
| Host | Ya | `sqlserver.internal` |
| Port | Ya | `1433` |
| Database | Ya | `finance` |
| Schema | Ya | `dbo` |
| SSL mode | Ya | `prefer` |
| Username | Ya | `replication_ro` |
| Secret reference | Ya | `mssql-finance-password` |

Contoh:

```json
{
  "tenant_id": "<tenant>",
  "name": "finance-sqlserver",
  "kind": "source",
  "driver": "mssql",
  "endpoint": {
    "host": "sqlserver.internal",
    "port": "1433",
    "database": "finance",
    "schema": "dbo",
    "sslmode": "prefer"
  },
  "username": "replication_ro",
  "secret_ref": "mssql-finance-password"
}
```

---

## 4. Oracle

Driver:

```text
oracle
```

Field:

| Field | Wajib | Contoh |
|---|---|---|
| Host | Ya | `oracle.internal` |
| Port | Ya | `1521` |
| Service name | Salah satu | `FINANCE` |
| SID | Salah satu | `ORCL` |
| Database | Alternatif service name | `FINANCE` |
| Schema | Ya | `REPORTING` |
| Username | Ya | `replication_ro` |
| Secret reference | Ya | `oracle-finance-password` |

Gunakan **service name atau SID**, bukan keduanya.

Contoh:

```json
{
  "tenant_id": "<tenant>",
  "name": "finance-oracle",
  "kind": "source",
  "driver": "oracle",
  "endpoint": {
    "host": "oracle.internal",
    "port": "1521",
    "service_name": "FINANCE",
    "schema": "REPORTING"
  },
  "username": "replication_ro",
  "secret_ref": "oracle-finance-password"
}
```

Oracle Instant Client tetap diperlukan untuk koneksi nyata.

---

## 5. MongoDB

Driver:

```text
mongodb
```

Field:

| Field | Wajib | Contoh |
|---|---|---|
| Host | Ya | `mongo.internal` |
| Port | Ya | `27017` |
| Database | Ya | `app` |
| Auth source | Ya | `admin` |
| SSL mode | Ya | `require` |
| Username | Ya | `mongo_reader` |
| Secret reference | Ya | `mongo-app-password` |

Worker membentuk URI MongoDB dari field tersebut. Jangan menulis password di
field endpoint.

Contoh:

```json
{
  "tenant_id": "<tenant>",
  "name": "mongo-app",
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
  "secret_ref": "mongo-app-password"
}
```

---

## 6. REST API

Driver:

```text
rest
```

Plugin: `tap-rest-api-msdk`

Role: **source**

Field:

| Field | Wajib | Contoh |
|---|---|---|
| API base URL | Ya | `https://api.example.com` |
| Path | Tidak | `/v1/records` |
| Stream name | Ya | `records` |
| Primary keys | Tidak | `id` |
| Records path | Ya | `$[*]` |
| Authentication | Ya | `no_auth`, `basic`, `api_key`, `bearer_token` |
| Username | Basic auth | `api-user` |
| API key header | API key auth | `X-API-KEY` |
| Secret reference | Basic/API key/Bearer | `example-api-token` |

Mapping secret:

- `basic`: secret menjadi password.
- `bearer_token`: secret menjadi bearer token.
- `api_key`: secret menjadi nilai header API key.
- `no_auth`: secret tidak diperlukan.

Contoh Bearer token:

```json
{
  "tenant_id": "<tenant>",
  "name": "example-api",
  "kind": "source",
  "driver": "rest",
  "endpoint": {
    "api_url": "https://api.example.com",
    "path": "/v1/records",
    "stream_name": "records",
    "primary_keys": ["id"],
    "records_path": "$[*]",
    "auth_method": "bearer_token"
  },
  "secret_ref": "example-api-token"
}
```

Form saat ini membuat satu stream REST. Konfigurasi lanjutan seperti beberapa
stream sekaligus, headers per stream, pagination token path, dan OAuth penuh
belum diekspos di UI.

---

## 7. CSV

Driver:

```text
csv
```

Plugin: `tap-csv`

Role: **source**

Field:

| Field | Wajib | Contoh |
|---|---|---|
| File or directory | Ya | `data/orders.csv` |
| Entity name | Ya | `orders` |
| Key columns | Tidak | `id, tenant_id` |
| Encoding | Tidak | `utf-8` |

Path relatif dihitung dari project Meltano tenant. File harus memiliki header
CSV. Jika path berupa directory, file di dalamnya dibaca rekursif.

Contoh:

```json
{
  "tenant_id": "<tenant>",
  "name": "orders-csv",
  "kind": "source",
  "driver": "csv",
  "endpoint": {
    "path": "data/orders.csv",
    "entity": "orders",
    "keys": ["id"],
    "encoding": "utf-8"
  }
}
```

CSV tidak memerlukan username, password, host, port, atau secret.

---

## 8. Google Sheets

Driver:

```text
google-sheets
```

Plugin: `tap-google-sheets`

Role: **source**

Field:

| Field | Wajib | Contoh |
|---|---|---|
| Spreadsheet ID | Ya | `1AbCdEfGh...` |
| Worksheet | Tidak | `Sheet1` |
| Range | Tidak | `A1:Z1000` |
| OAuth credentials secret reference | Ya | `google-sheets-oauth` |

Secret reference harus menghasilkan JSON:

```json
{
  "client_id": "google-client-id",
  "client_secret": "google-client-secret",
  "refresh_token": "google-refresh-token"
}
```

Contoh:

```json
{
  "tenant_id": "<tenant>",
  "name": "orders-sheet",
  "kind": "source",
  "driver": "google-sheets",
  "endpoint": {
    "sheet_id": "1AbCdEfGh...",
    "child_sheet_name": "Orders",
    "range": "A1:Z1000"
  },
  "secret_ref": "google-sheets-oauth"
}
```

Google Sheets tidak membutuhkan host, database, username, atau password
database.

---

## 9. Amazon S3

Driver:

```text
s3
```

Plugin: `target-s3`

Role: **destination**

Field:

| Field | Wajib | Contoh |
|---|---|---|
| Bucket | Ya | `analytics-exports` |
| Region | Ya | `ap-southeast-1` |
| Prefix | Tidak | `warehouse/` |
| File format | Ya | `parquet`, `csv`, `jsonl` |
| Access key ID | Opsional | `AKIA...` |
| Secret access key reference | Opsional | `aws-secret-access-key` |
| Custom endpoint | Opsional | `https://s3.example.com` |

Jika access key dan secret reference kosong, worker mencoba mekanisme credential
AWS default seperti IAM role/profile.

Contoh:

```json
{
  "tenant_id": "<tenant>",
  "name": "warehouse-s3",
  "kind": "destination",
  "driver": "s3",
  "endpoint": {
    "bucket": "analytics-exports",
    "region": "ap-southeast-1",
    "prefix": "warehouse/",
    "format": "parquet"
  },
  "username": "AKIA...",
  "secret_ref": "aws-secret-access-key"
}
```

---

## 10. DuckDB

Driver:

```text
duckdb
```

Plugin: `target-duckdb`

Role: **destination**

Field:

| Field | Wajib | Contoh |
|---|---|---|
| DuckDB file path | Ya | `output/warehouse.duckdb` |
| Default schema | Ya | `main` |

Contoh:

```json
{
  "tenant_id": "<tenant>",
  "name": "analytics-duckdb",
  "kind": "destination",
  "driver": "duckdb",
  "endpoint": {
    "path": "output/warehouse.duckdb",
    "schema": "main"
  }
}
```

DuckDB tidak memerlukan username, password, host, port, atau secret untuk file
lokal.

---

## Opsi load PostgreSQL dan SQL Server

Untuk destination `postgres` atau `mssql`, field `options.load_method`:

| Nilai | Perilaku |
|---|---|
| `upsert` | Insert row baru dan update row dengan key yang sama |
| `overwrite` | Kosongkan/recreate tabel sebelum load pertama |
| `append-only` | Selalu insert; berisiko duplikat jika dijalankan berulang |

Contoh:

```json
{
  "options": {
    "load_method": "upsert"
  }
}
```

## Troubleshooting umum

### Google Sheets meminta username/password

Itu adalah bug UI lama. Form sekarang hanya meminta Spreadsheet ID, worksheet,
range, dan OAuth secret reference.

### CSV atau DuckDB tidak bisa disimpan

Pastikan path diisi. Keduanya tidak membutuhkan secret.

### Secret tidak ditemukan

Worker akan menampilkan:

```text
secret "<ref>" tidak ditemukan; set CP_SECRET_<REF>
```

Set environment tersebut pada proses worker lalu restart worker.

### Google Sheets gagal membaca secret

Pastikan nilai secret adalah JSON valid dengan `client_id`, `client_secret`, dan
`refresh_token`.

### S3 menggunakan IAM role

Kosongkan access key dan secret reference jika worker berjalan pada environment
AWS dengan role/profile yang memiliki akses bucket.

### Oracle gagal memuat `cx_Oracle`

Pastikan Oracle Instant Client tersedia dan plugin Oracle memakai Python 3.10.
