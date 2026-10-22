# Deployment Docker ke Server

Dokumen ini adalah runbook eksekusi stack yang sama di server. Nilai seperti
domain, IP, dan akun database harus disesuaikan dengan server yang dipilih.

> **Penting:** aplikasi belum memiliki autentikasi pengguna. Jangan membuka UI
> atau API langsung ke internet. Tahap awal harus melalui SSH tunnel, VPN, atau
> reverse proxy dengan TLS dan Basic Auth.

## 1. Target deployment

Stack tetap memakai database eksternal dan menjalankan service berikut:

| Service | Port host |
|---|---|
| `api` | `127.0.0.1:8080` |
| `ui` | `127.0.0.1:3000` |
| `worker` | Tanpa port publik |
| `provision` | Satu kali, tanpa port publik |

Port hanya di-bind ke loopback host. Reverse proxy di host boleh meneruskan
traffic ke UI, tetapi API tidak perlu dibuka langsung.

## 2. Informasi yang perlu disiapkan sebelum sesi server

- Alamat IP dan akses SSH server
- OS dan arsitektur server
- Domain atau subdomain, jika akan memakai HTTPS
- Alamat PostgreSQL yang dapat dijangkau server
- Kredensial dua database
- Commit atau branch repository yang akan di-deploy
- Daftar connector dan tenant awal
- Metode akses: SSH tunnel, VPN, atau domain dengan Basic Auth

## 3. Spesifikasi minimum yang disarankan

| Resource | Minimum | Disarankan |
|---|---|---|
| CPU | 2 vCPU | 4 vCPU |
| RAM | 4 GB | 8 GB |
| Disk | 20 GB | 40 GB atau lebih |
| OS | Ubuntu 22.04/24.04 | Ubuntu LTS terbaru |
| Akses | SSH + sudo | SSH key, firewall aktif |

Build image dan instalasi plugin Meltano membutuhkan resource lebih besar
daripada runtime normal.

## 4. Siapkan DNS dan firewall

Pilihan yang aman untuk tahap awal:

- Hanya buka port `22` untuk SSH.
- Akses UI melalui SSH tunnel.
- Belum perlu membuka `80` atau `443`.

Contoh SSH tunnel dari komputer local:

```powershell
ssh -L 3000:127.0.0.1:3000 -L 8080:127.0.0.1:8080 user@SERVER_IP
```

Setelah tunnel aktif, buka `http://localhost:3000` di komputer local. Traffic
melewati SSH dan port aplikasi tidak terbuka ke internet.

Jika nanti memakai domain, arahkan DNS ke IP server dan buka `80` serta `443`
setelah reverse proxy siap.

## 5. Izinkan akses server ke database

Database eksternal harus mengizinkan koneksi dari IP server.

- PostgreSQL managed service: tambahkan IP server ke allowlist.
- PostgreSQL remote: konfigurasi `pg_hba.conf` dan firewall untuk user aplikasi.
- PostgreSQL di server yang sama: gunakan `host.docker.internal` dari container.

Gunakan TLS bila database mendukung:

```dotenv
CP_DATABASE_URL=postgres://controlplane:PASSWORD@DB_HOST:5432/controlplane?sslmode=require
CP_SYSTEM_DB_URI=postgres://meltano:PASSWORD@DB_HOST:5432/meltano_system?sslmode=require
```

## 6. Install Docker di server

Gunakan dokumentasi resmi Docker untuk distribusi Linux yang dipakai. Setelah
instalasi:

```bash
docker version
docker compose version
docker info
```

Pastikan Docker dan Compose menggunakan plugin `docker compose`, bukan binary
lama `docker-compose`.

## 7. Ambil source code

Contoh lokasi deployment:

```bash
sudo mkdir -p /opt/meltano-controlplane
sudo chown "$USER":"$USER" /opt/meltano-controlplane

git clone REPOSITORY_URL /opt/meltano-controlplane
cd /opt/meltano-controlplane
git checkout COMMIT_ATAU_TAG_YANG_DIINGINKAN
```

Jangan deploy langsung dari branch yang terus berubah. Catat commit yang sedang
aktif supaya rollback dapat dilakukan.

## 8. Buat konfigurasi server

```bash
cp docker.env.example .env.docker
chmod 600 .env.docker
```

Isi konfigurasi. Untuk server gunakan:

```dotenv
CP_ALLOW_PLAINTEXT_SECRETS=false
CP_LOG_LEVEL=info
CP_BOOTSTRAP_TENANT=acme
CP_BOOTSTRAP_ENVIRONMENT=dev
```

Secret connection harus memakai `secret_ref`. Nilai secret dapat disediakan
lewat environment worker untuk tahap awal:

```dotenv
CP_SECRET_SOURCE_PROD=...
CP_SECRET_DEST_PROD=...
```

Untuk jangka panjang, pindahkan secret ke Vault atau cloud secret manager.

Jangan commit `.env.docker`. File tersebut sudah diabaikan oleh Git.

## 9. Validasi dan start

```bash
cd /opt/meltano-controlplane
docker compose config --quiet
bash controlplane/deploy/deploy.sh
```

Script tersebut membangun image, menjalankan service, menunggu provisioning,
memeriksa `/readyz`, dan memastikan `api`, `worker`, serta `ui` berjalan.

Pantau:

```bash
docker compose ps
docker compose logs -f api provision worker ui
```

Status akhir:

- `api`: healthy/running
- `worker`: running
- `ui`: running
- `provision`: exited dengan kode `0`

Verifikasi di server:

```bash
bash controlplane/deploy/check.sh
```

## 10. Akses UI

Pilihan 1, paling aman untuk awal:

```powershell
ssh -L 3000:127.0.0.1:3000 user@SERVER_IP
```

Buka `http://localhost:3000`.

Pilihan 2, domain dengan Nginx/Caddy dan TLS:

1. Pasang reverse proxy di host server.
2. Arahkan DNS domain ke IP server, lalu konfigurasi reverse proxy ke
   `127.0.0.1:3000`.
3. Aktifkan HTTPS.
4. Tambahkan Basic Auth atau identitas akses lain.
5. Jangan meneruskan port API langsung ke publik.

Template Caddyfile tersedia di
[`../deploy/Caddyfile.example`](../deploy/Caddyfile.example):

```caddyfile
meltano.example.com {
    encode zstd gzip

    basic_auth {
        admin HASH_PASSWORD_DARI_CADDY
    }

    reverse_proxy 127.0.0.1:3000
}
```

Buat hash password:

```bash
caddy hash-password --plaintext 'PASSWORD_YANG_DIPILIH'
```

Periksa konfigurasi dan reload:

```bash
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

## 11. Firewall

Untuk mode SSH tunnel:

```text
allow 22/tcp
deny 3000/tcp
deny 8080/tcp
```

Untuk mode domain:

```text
allow 22/tcp
allow 80/tcp
allow 443/tcp
deny 3000/tcp
deny 8080/tcp
```

Pastikan binding container tetap `127.0.0.1`. Periksa dengan:

```bash
docker compose port ui 80
docker compose port api 8080
```

## 12. Operasi harian

Status:

```bash
docker compose ps
```

Health:

```bash
curl --fail http://127.0.0.1:8080/readyz
```

Log:

```bash
docker compose logs --tail=200 api worker ui
docker compose logs --tail=200 provision
```

Restart:

```bash
docker compose restart api worker ui
```

Penggunaan disk:

```bash
docker system df
docker compose images
```

## 13. Backup

Backup harus mencakup:

1. Database `controlplane`
2. Database `meltano_system`
3. Volume `meltano-controlplane_tenants`
4. Volume `meltano-controlplane_meltano-home`

Jalankan script backup dari root repository:

```bash
bash controlplane/deploy/backup.sh
```

Script membuat dump dua database dan archive dua volume di
`backups/<timestamp>/`. Jika versi PostgreSQL server lebih baru dari image
client default, set `POSTGRES_CLIENT_IMAGE` ke versi yang sesuai.

Uji restore di lingkungan terpisah sebelum benar-benar dibutuhkan.

## 14. Update aplikasi

Sebelum update:

1. Catat commit aktif.
2. Backup database dan volume.
3. Pastikan tidak ada run kritis yang sedang berjalan.

Contoh proses:

```bash
git fetch --all --tags
git checkout COMMIT_ATAU_TAG_BARU
docker compose up --build -d
docker compose ps
curl --fail http://127.0.0.1:8080/readyz
```

Migrasi control plane berjalan otomatis saat API start. Jangan menjalankan
`docker compose down -v`.

## 15. Rollback

Jika update gagal:

```bash
git checkout COMMIT_SEBELUMNYA
docker compose up --build -d
```

Rollback kode tidak otomatis membatalkan migrasi database. Karena itu backup
database harus dibuat sebelum update yang membawa migrasi baru.

## 16. Checklist keamanan

- [ ] Port `3000` dan `8080` hanya terikat ke `127.0.0.1`
- [ ] Akses publik memakai TLS dan Basic Auth, VPN, atau SSH tunnel
- [ ] `CP_ALLOW_PLAINTEXT_SECRETS=false`
- [ ] Password database tidak masuk Git
- [ ] Database hanya menerima koneksi dari IP server
- [ ] Backup database dan volume terjadwal
- [ ] Docker daemon tidak diekspos ke internet
- [ ] Log dan disk dipantau

## 17. Checklist saat sesi deployment nanti

- [ ] Konfirmasi IP, domain, dan akses SSH
- [ ] Konfirmasi PostgreSQL serta dua nama database
- [ ] Pastikan server dapat menghubungi database
- [ ] Tentukan metode akses UI
- [ ] Siapkan `.env.docker` dengan permission `600`
- [ ] Build image di server
- [ ] Tunggu provisioning tenant selesai
- [ ] Verifikasi health dan UI
- [ ] Buat source/destination connection
- [ ] Jalankan pipeline uji
- [ ] Simpan backup awal dan catat commit deployment
