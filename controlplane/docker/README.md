# Docker stack

Stack ini menjalankan seluruh aplikasi dengan satu perintah Compose:

| Service | Isi |
|---|---|
| `api` | HTTP API Go |
| `worker` | Worker Go, dengan Python + Meltano di image yang sama |
| `provision` | Bootstrap satu tenant dan plugin Meltano, lalu selesai |
| `ui` | Build Svelte yang disajikan Nginx |

PostgreSQL **tidak** dijalankan oleh stack ini. Database harus eksternal dan
harus sudah dibuat sebelum stack dinyalakan.

Panduan langkah demi langkah:

- Local: [`../docs/docker-local-step-by-step.md`](../docs/docker-local-step-by-step.md)
- Server: [`../docs/docker-server-deployment.md`](../docs/docker-server-deployment.md)
- Oracle Linux: [`../docs/docker-oracle-linux-server.md`](../docs/docker-oracle-linux-server.md)

Script siap pakai:

- Deploy: [`../deploy/deploy.sh`](../deploy/deploy.sh)
- Health check: [`../deploy/check.sh`](../deploy/check.sh)
- Backup: [`../deploy/backup.sh`](../deploy/backup.sh)

## Persiapan

1. Buat dua database PostgreSQL kosong dengan user yang boleh membuat tabel dan
   extension `pgcrypto`.

   Contoh bila Anda memiliki akses administratif ke server:

   ```sql
   CREATE DATABASE controlplane;
   CREATE DATABASE meltano_system;
   ```

2. Dari root repository:

   ```bash
   cp docker.env.example .env.docker
   ```

3. Isi `CP_DATABASE_URL` untuk store control plane dan `CP_SYSTEM_DB_URI` untuk
   system database Meltano. Jika PostgreSQL berjalan di host Docker, gunakan
   `host.docker.internal`, bukan `localhost`.
4. Nyalakan stack:

   ```bash
   bash controlplane/deploy/deploy.sh
   bash controlplane/deploy/check.sh
   ```

   Alternatif tanpa script: `docker compose up --build -d`.

UI tersedia di `http://localhost:3000`, sedangkan API langsung tersedia di
`http://localhost:8080`. Browser hanya perlu membuka UI karena Nginx
mem-proxy-kan `/v1`, `/healthz`, dan `/readyz` ke API.

Build pertama mengunduh dependency Python, Go, Node, dan image dasar sehingga
dapat memerlukan beberapa menit. Setelah image selesai dibangun, start berikutnya
menggunakan cache BuildKit.

## Provisioning tenant

Service `provision` otomatis membuat tenant pertama dari nilai
`CP_BOOTSTRAP_TENANT`, `CP_BOOTSTRAP_ENVIRONMENT`,
`CP_BOOTSTRAP_EXTRACTOR`, dan `CP_BOOTSTRAP_LOADER`. Dengan
`CP_BOOTSTRAP_INSTALL_ALL_CONNECTORS=true`, seluruh connector katalog dipasang.
Tenant juga didaftarkan ke API agar langsung muncul di UI. Setelah berhasil,
marker `.docker-provisioned` disimpan di volume dan instalasi plugin dilewati
pada `docker compose up` berikutnya; registrasi tenant tetap diverifikasi.

Untuk menambah tenant atau mengganti plugin:

```bash
docker compose run --rm --entrypoint /usr/local/bin/provision provision \
  -tenant tenant-baru \
  -extractor tap-mysql \
  -loader target-postgres
```

Project tenant disimpan di volume `meltano-controlplane_tenants`. Homes,
cache, dan log Meltano disimpan di volume `meltano-controlplane_meltano-home`.

## Pemeriksaan

```bash
docker compose ps
docker compose logs -f api provision worker ui
curl http://localhost:8080/readyz
```

Database eksternal harus dapat dijangkau dari container. Jika memakai PostgreSQL
di host Docker Desktop, `localhost` di dalam container menunjuk container itu
sendiri; gunakan `host.docker.internal`, termasuk pada endpoint koneksi
source/destination yang diisi lewat UI.

`CP_ALLOW_PLAINTEXT_SECRETS` default `false`. Untuk pengujian UI dengan input
password langsung, ubah nilainya menjadi `true` di `.env.docker`. Untuk
production, gunakan `secret_ref` pada koneksi dan sediakan nilai worker melalui
variabel `CP_SECRET_<REF>` atau secret manager.
