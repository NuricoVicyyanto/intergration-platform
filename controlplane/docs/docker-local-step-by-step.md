# Menjalankan Docker Stack di Local secara Step by Step

Panduan ini menjalankan seluruh aplikasi dari root repository:

- API HTTP Go pada `http://localhost:8080`
- Worker Go yang juga membawa Python dan Meltano
- Provisioner tenant
- UI Svelte pada `http://localhost:3000`

PostgreSQL tidak dijalankan sebagai container. Control plane dan Meltano
menghubungi database eksternal melalui DSN pada `.env.docker`.

## 0. Yang harus disiapkan

Pastikan:

1. Docker Desktop sudah terpasang dan Docker Engine berstatus running.
2. Docker Compose tersedia.
3. Repository ini sudah tersedia di komputer local.
4. Ada PostgreSQL yang dapat dihubungi dari container.
5. Ruang disk kosong minimal sekitar 8 GB untuk image, dependency, dan plugin.

Periksa Docker dari PowerShell:

```powershell
docker version
docker compose version
docker info
```

Jika `docker info` gagal, buka Docker Desktop dan tunggu sampai status engine
running sebelum melanjutkan.

## 1. Siapkan dua database PostgreSQL

Gunakan dua database yang berbeda:

| Database | Dipakai oleh | Contoh nama |
|---|---|---|
| Job store control plane | API dan worker Go | `controlplane` |
| System database Meltano | Meltano, state, dan riwayat run | `meltano_system` |

Jangan memakai satu database untuk keduanya karena control plane dan Meltano
sama-sama membuat tabel bernama `runs`.

Jika Anda memiliki akses administratif ke PostgreSQL, contoh pembuatannya:

```sql
CREATE ROLE controlplane LOGIN PASSWORD 'ganti-password-controlplane';
CREATE ROLE meltano LOGIN PASSWORD 'ganti-password-meltano';

CREATE DATABASE controlplane OWNER controlplane;
CREATE DATABASE meltano_system OWNER meltano;
```

Kedua user database perlu izin membuat tabel dan extension `pgcrypto`.
Extension tersebut dijalankan otomatis oleh migrasi control plane saat API
pertama kali start.

## 2. Tentukan alamat database dari container

`localhost` di dalam container menunjuk container itu sendiri, bukan komputer
host.

| Lokasi PostgreSQL | Host yang dipakai di DSN |
|---|---|
| PostgreSQL di komputer yang sama dengan Docker Desktop | `host.docker.internal` |
| PostgreSQL remote atau managed service | hostname atau IP server database |
| PostgreSQL di server Linux yang sama dengan Docker | `host.docker.internal` |

Jika password mengandung karakter khusus seperti `@`, `:`, `/`, atau `#`,
password harus di-URL-encode di dalam DSN.

## 3. Buat `.env.docker`

Jalankan dari root repository:

```powershell
Copy-Item docker.env.example .env.docker
```

Buka `.env.docker`, lalu minimal ubah dua DSN berikut:

```dotenv
CP_DATABASE_URL=postgres://controlplane:PASSWORD@host.docker.internal:5432/controlplane?sslmode=disable
CP_SYSTEM_DB_URI=postgres://meltano:PASSWORD@host.docker.internal:5432/meltano_system?sslmode=disable
```

Tenant awal juga dapat diatur di file yang sama:

```dotenv
CP_BOOTSTRAP_TENANT=acme
CP_BOOTSTRAP_ENVIRONMENT=dev
CP_BOOTSTRAP_INSTALL_ALL_CONNECTORS=true
CP_BOOTSTRAP_EXTRACTOR=tap-postgres
CP_BOOTSTRAP_LOADER=target-postgres
```

`CP_BOOTSTRAP_*` menentukan project tenant, environment Meltano, dan plugin yang
diprovision. Provisioner juga mendaftarkan tenant tersebut ke API sehingga
langsung muncul di UI. Pipeline, connection, dan source table tetap dibuat lewat
UI atau API.

Seluruh 15 connector terpasang secara default, bahkan bila variabel
`CP_BOOTSTRAP_INSTALL_ALL_CONNECTORS` tidak ditulis: PostgreSQL, MySQL, SQL
Server, Oracle, MongoDB, REST API, CSV, Google Sheets, S3, dan DuckDB.
Instalasi pertama membutuhkan waktu serta disk lebih besar. Set nilainya ke
`false` untuk hanya memasang pasangan `CP_BOOTSTRAP_EXTRACTOR` dan
`CP_BOOTSTRAP_LOADER`.

### Pilihan credential untuk uji local

Cara paling sederhana untuk uji local adalah mengaktifkan password koneksi dari
UI:

```dotenv
CP_ALLOW_PLAINTEXT_SECRETS=true
```

Nilai ini membuat password koneksi tersimpan di database control plane. Jangan
gunakan nilai tersebut untuk production.

Alternatif yang lebih aman adalah menggunakan `secret_ref` dan mendefinisikan
nilainya sebagai environment variable worker:

```dotenv
CP_SECRET_DEMO=dummy
```

`secret_ref=demo` pada koneksi akan dibaca worker dari `CP_SECRET_DEMO`.

## 4. Validasi konfigurasi Compose

Masih dari root repository:

```powershell
Test-Path .env.docker
docker compose config --quiet
```

`Test-Path` harus menghasilkan `True`. Perintah Compose tidak boleh menampilkan
error. Jika gagal, pastikan `.env.docker` berada di root repository dan periksa
kembali format `KEY=value` di dalamnya.

## 5. Build dan jalankan stack

```powershell
docker compose up --build -d
```

Atau gunakan script deployment yang juga menunggu provisioning dan health check:

```powershell
bash controlplane/deploy/deploy.sh
```

Build pertama dapat memerlukan beberapa menit karena mengunduh dependency
Python, Go, Node, image dasar, dan kemudian plugin Meltano.

Urutan start yang normal:

1. `api` start dan menjalankan migrasi job store.
2. `provision` membuat project tenant dan menginstal extractor/loader.
3. `worker` start setelah API siap dan provisioning berhasil.
4. `ui` start setelah API sehat.
5. Container `provision` berhenti dengan exit code `0`. Ini normal.

## 6. Pantau proses pertama

Tampilkan status:

```powershell
docker compose ps
```

Status yang diharapkan setelah provisioning selesai:

- `api`, `worker`, dan `ui`: `running`
- `provision`: `exited (0)`

Ikuti log provisioning:

```powershell
docker compose logs -f provision
```

Jika provisioning masih berjalan, log akan menunjukkan proses `meltano add` dan
`meltano install`. Jangan jalankan `docker compose down -v` saat proses ini
berlangsung.

Jika ada masalah, lihat log API:

```powershell
docker compose logs -f api
```

## 7. Verifikasi API dan UI

Periksa kesiapan API:

```powershell
Invoke-RestMethod http://127.0.0.1:8080/readyz
```

Respons yang diharapkan berisi:

```json
{
  "ready": true,
  "checks": {
    "job_store": {
      "ok": true
    }
  }
}
```

Periksa versi Meltano yang terdeteksi:

```powershell
Invoke-RestMethod http://127.0.0.1:8080/v1/version
```

Buka UI:

```powershell
Start-Process http://localhost:3000
```

UI menggunakan Nginx untuk mem-proxy `/v1`, `/healthz`, dan `/readyz` ke API.
Browser cukup membuka port `3000`.

## 8. Buat koneksi dan pipeline pertama

Setelah UI terbuka:

1. Pastikan tenant hasil bootstrap, misalnya `acme`, muncul.
2. Buka halaman **Connections**.
3. Buat connection `source`, misalnya PostgreSQL sumber.
4. Buat connection `destination`, misalnya PostgreSQL tujuan.
5. Isi host database dengan alamat yang dapat dijangkau dari container.
6. Simpan connection.
7. Buka halaman **Pipelines** dan buat pipeline dari source ke destination.
8. Pilih tabel atau stream yang akan direplikasi.
9. Validasi pipeline.
10. Jalankan pipeline dan pantau status/log pada halaman run.

Referensi field koneksi tersedia di
[`connection-types.md`](connection-types.md). Mekanisme filter tabel dan baris
tersedia di
[`source-selection-dan-row-filter.md`](source-selection-dan-row-filter.md).

## 9. Stop, start, dan restart

Stop tanpa menghapus data:

```powershell
docker compose stop
```

Start kembali:

```powershell
docker compose start
```

Restart setelah mengubah `.env.docker`:

```powershell
docker compose up -d --force-recreate api worker
```

Lihat log semua service:

```powershell
docker compose logs -f --tail=200
```

### Jangan gunakan perintah ini tanpa sengaja

```powershell
docker compose down -v
```

Opsi `-v` menghapus volume tenant dan Meltano. Project tenant, plugin yang sudah
diinstal, state, dan cache Meltano akan hilang.

`docker compose down` tanpa `-v` aman untuk menghentikan dan menghapus container
karena volume tetap disimpan.

## 10. Lokasi data

Nama volume mengikuti nama project Compose:

| Volume | Isi |
|---|---|
| `meltano-controlplane_tenants` | Satu project Meltano per tenant |
| `meltano-controlplane_meltano-home` | Home, cache, config, dan log Meltano |

Lihat volume:

```powershell
docker volume ls | Select-String meltano-controlplane
```

Data bisnis hasil replikasi tetap berada di database source dan destination,
bukan di volume ini.

## 11. Update kode aplikasi

Ambil perubahan terbaru, lalu build ulang:

```powershell
git pull
docker compose up --build -d
```

Migrasi control plane berjalan otomatis saat API start. Jangan hapus volume
tenant ketika melakukan update.

## 12. Menambah tenant atau plugin lain

Jalankan provisioner manual:

```powershell
docker compose run --rm --entrypoint /usr/local/bin/provision provision `
  -tenant tenant-baru `
  -extractor tap-mysql `
  -loader target-postgres
```

Contoh plugin lain dapat dilihat di
[`menambah-konektor.md`](menambah-konektor.md).

## 13. Troubleshooting

| Gejala | Kemungkinan penyebab | Tindakan |
|---|---|---|
| API terus restart | DSN salah atau database tidak terjangkau | `docker compose logs api`; cek host, port, user, password, SSL |
| `connection refused` ke database | Memakai `localhost` dari dalam container | Gunakan `host.docker.internal` atau hostname database yang benar |
| Provision gagal mengunduh plugin | Container tidak punya akses internet atau nama plugin salah | Periksa `docker compose logs provision` dan koneksi internet |
| `permission denied` membuat extension | User database bukan owner dan tidak boleh membuat extension | Berikan hak yang sesuai atau buat `pgcrypto` sebagai admin |
| Port 3000 atau 8080 sudah dipakai | Ada proses lain di host | Hentikan proses tersebut atau ubah mapping port Compose |
| UI terbuka tetapi API error | API belum healthy atau proxy tidak menemukan service `api` | Cek `docker compose ps` dan log `api` |
| Worker tidak start | `provision` belum selesai atau exit bukan 0 | Cek `docker compose ps` dan `docker compose logs provision` |
| Run gagal karena secret tidak ada | `secret_ref` tidak cocok dengan environment worker | Pastikan `CP_SECRET_<REF>` tersedia di `.env.docker` |
| Perubahan `.env.docker` tidak terlihat | Container belum dibuat ulang | `docker compose up -d --force-recreate api worker` |

## 14. Checklist selesai

- [ ] `docker info` berhasil
- [ ] Database `controlplane` dan `meltano_system` tersedia
- [ ] `.env.docker` sudah diisi dan tidak di-commit
- [ ] `docker compose config --quiet` berhasil
- [ ] `docker compose up --build -d` berhasil
- [ ] `provision` exited dengan kode `0`
- [ ] `/readyz` mengembalikan `ready: true`
- [ ] UI di `http://localhost:3000` dapat dibuka
- [ ] Source dan destination connection berhasil dibuat
- [ ] Pipeline dapat divalidasi dan dijalankan
