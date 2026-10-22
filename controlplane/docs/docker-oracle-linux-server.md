# Install Docker Stack di Oracle Linux

Panduan cepat untuk Oracle Linux 8 atau 9. Stack menjalankan API Go, worker
Go + Python/Meltano, UI Svelte, dan provisioning seluruh connector. PostgreSQL
tetap berada di luar stack.

## 1. Spesifikasi server

Disarankan:

| Resource | Minimum | Disarankan |
|---|---|---|
| CPU | 2 vCPU | 4 vCPU |
| RAM | 4 GB | 8 GB |
| Disk | 30 GB | 40 GB atau lebih |
| OS | Oracle Linux 8/9 | Oracle Linux 9 terbaru |
| Database | PostgreSQL eksternal | PostgreSQL 16/17 |

Image aplikasi berisi Python, Go, Node build output, dan library connector.
Instalasi 15 connector pertama kali membutuhkan bandwidth dan disk lebih besar.

## 2. Install Docker CE

Jalankan sebagai user yang memiliki akses `sudo`:

```bash
sudo dnf update -y
sudo dnf install -y dnf-plugins-core git tmux curl

sudo dnf config-manager --add-repo \
  https://download.docker.com/linux/centos/docker-ce.repo

sudo dnf install -y \
  docker-ce \
  docker-ce-cli \
  containerd.io \
  docker-buildx-plugin \
  docker-compose-plugin

sudo systemctl enable --now docker
```

Masukkan user deploy ke group Docker:

```bash
sudo usermod -aG docker "$USER"
newgrp docker
```

Verifikasi:

```bash
docker version
docker compose version
docker info
```

Jika `docker info` memberi `permission denied`, logout lalu login kembali atau
jalankan `newgrp docker` pada terminal baru.

> User dalam group `docker` memiliki akses setara root terhadap Docker daemon.
> Batasi akses SSH dan jangan mengekspos Docker socket ke internet.

## 3. Siapkan PostgreSQL eksternal

Buat dua database berbeda:

```sql
CREATE DATABASE meltano_controlplane;
CREATE DATABASE meltano_system_docker;
```

Pastikan user database memiliki hak membuat tabel dan extension `pgcrypto`.

Alamat database dilihat dari dalam container:

| Lokasi database | Host pada DSN |
|---|---|
| Server PostgreSQL remote | hostname atau IP database |
| PostgreSQL di host Oracle Linux yang sama | `host.docker.internal` |
| PostgreSQL managed | hostname provider |

Jangan gunakan `localhost` untuk database yang berada di luar container.

## 4. Clone repository

```bash
sudo mkdir -p /opt/meltano-controlplane
sudo chown "$USER":"$USER" /opt/meltano-controlplane

git clone https://github.com/NuricoVicyyanto/intergration-platform.git \
  /opt/meltano-controlplane

cd /opt/meltano-controlplane
```

Untuk deployment stabil, checkout commit atau tag yang sudah diuji:

```bash
git checkout COMMIT_ATAU_TAG
```

## 5. Buat `.env.docker`

```bash
cp docker.env.example .env.docker
chmod 600 .env.docker
vi .env.docker
```

Isi konfigurasi minimal:

```dotenv
CP_DATABASE_URL=postgres://USER:PASSWORD@DB_HOST:5432/meltano_controlplane?sslmode=require
CP_SYSTEM_DB_URI=postgres://USER:PASSWORD@DB_HOST:5432/meltano_system_docker?sslmode=require

CP_BOOTSTRAP_TENANT=acme
CP_BOOTSTRAP_ENVIRONMENT=dev
CP_BOOTSTRAP_INSTALL_ALL_CONNECTORS=true

CP_ALLOW_PLAINTEXT_SECRETS=false
CP_LOG_LEVEL=info
```

Seluruh 15 connector dipasang secara default. Baris
`CP_BOOTSTRAP_INSTALL_ALL_CONNECTORS=true` boleh dihilangkan; set ke `false`
hanya jika server memang dibatasi ke dua connector awal.

Jika password mengandung karakter khusus, URL-encode password tersebut.
Jangan commit `.env.docker` karena berisi kredensial.

Validasi Compose:

```bash
docker compose config --quiet
```

## 6. Jalankan deployment

Gunakan `tmux` agar deployment tidak berhenti ketika koneksi SSH terputus:

```bash
tmux new -s deploy

cd /opt/meltano-controlplane
bash controlplane/deploy/deploy.sh
bash controlplane/deploy/check.sh
```

Detach dari tmux:

```text
Ctrl+B kemudian D
```

Masuk kembali:

```bash
tmux attach -t deploy
```

Script deployment:

1. Build image Go/Python/Svelte.
2. Menjalankan PostgreSQL migration control plane.
3. Memprovision tenant dan seluruh 15 connector.
4. Mendaftarkan tenant ke API.
5. Menjalankan worker dan UI.
6. Memeriksa `/readyz` serta status container.

Instalasi pertama dapat memerlukan beberapa menit sampai puluhan menit
tergantung kecepatan internet dan server.

## 7. Verifikasi

```bash
docker compose ps
docker compose logs --tail=100 api provision worker ui
bash controlplane/deploy/check.sh
```

Status akhir:

- `api`: healthy
- `worker`: running
- `ui`: running
- `provision`: exited dengan kode `0`

Periksa API:

```bash
curl --fail http://127.0.0.1:8080/readyz
curl --fail http://127.0.0.1:8080/v1/version
```

## 8. Akses UI dengan aman

Port `3000` dan `8080` sudah di-bind ke loopback server. Cara paling aman adalah
SSH tunnel:

```bash
ssh -L 3000:127.0.0.1:3000 USER@IP_SERVER
```

Buka di komputer local:

```text
http://localhost:3000
```

Aplikasi belum memiliki autentikasi pengguna. Jangan membuka port `3000` atau
`8080` langsung ke internet. Untuk akses public, gunakan reverse proxy dengan
TLS dan Basic Auth, VPN, atau identity-aware proxy.

## 9. Firewalld

Untuk mode SSH tunnel, hanya port SSH yang perlu dibuka:

```bash
sudo firewall-cmd --permanent --add-service=ssh
sudo firewall-cmd --reload
sudo firewall-cmd --list-all
```

Pastikan port aplikasi tidak terbuka:

```bash
sudo ss -lntp | grep -E ':3000|:8080'
```

Binding yang benar menampilkan `127.0.0.1:3000` dan `127.0.0.1:8080`.

## 10. SELinux

Panduan ini menggunakan named Docker volume, bukan bind mount direktori host,
sehingga umumnya tidak memerlukan perubahan label SELinux.

Jika container mendapat error permission padahal volume sudah benar:

```bash
sudo ausearch -m avc -ts recent
getenforce
```

Jangan langsung menonaktifkan SELinux. Perbaiki label atau gunakan named volume
yang sesuai.

## 11. Operasi harian

Status:

```bash
docker compose ps
```

Log:

```bash
docker compose logs --tail=200 api worker ui
```

Restart aplikasi:

```bash
docker compose restart api worker ui
```

Stop tanpa menghapus data:

```bash
docker compose stop
```

Start kembali:

```bash
docker compose start
```

Jangan gunakan `docker compose down -v` kecuali benar-benar ingin menghapus
volume tenant dan cache Meltano.

## 12. Backup

```bash
cd /opt/meltano-controlplane
bash controlplane/deploy/backup.sh
```

Backup berisi dua database dan dua volume Meltano. Simpan salinan backup di
storage terpisah dari server.

Pilihan image client PostgreSQL:

```bash
POSTGRES_CLIENT_IMAGE=postgres:17-alpine \
  bash controlplane/deploy/backup.sh
```

## 13. Update

```bash
cd /opt/meltano-controlplane

# Backup sebelum update
bash controlplane/deploy/backup.sh

git fetch --all --tags
git checkout COMMIT_ATAU_TAG_BARU
bash controlplane/deploy/deploy.sh
bash controlplane/deploy/check.sh
```

Migrasi database berjalan otomatis ketika API start.

## 14. Troubleshooting Oracle Linux

| Masalah | Penyebab | Solusi |
|---|---|---|
| `docker: command not found` | Docker belum terpasang | Ulangi langkah Docker CE |
| `permission denied` ke daemon | User belum masuk group Docker | `newgrp docker` atau login ulang |
| `docker compose` tidak dikenali | Plugin Compose belum ada | Install `docker-compose-plugin` |
| Repository Docker tidak ditemukan | Repo belum ditambahkan | Tambahkan repo `docker-ce.repo` |
| Container tidak bisa menghubungi DB | `localhost` dipakai dari container | Gunakan host DB atau `host.docker.internal` |
| Build berhenti karena disk penuh | Image dan plugin besar | Periksa `df -h` dan `docker system df` |
| Build berhenti karena OOM | RAM server kurang | Tambah RAM/swap atau build di mesin lain |
| Provision gagal | Internet, plugin, atau library connector | `docker compose logs provision` |
| UI hanya bisa dari server | Port di-bind ke loopback | Gunakan SSH tunnel atau reverse proxy |

## 15. Checklist selesai

- [ ] Docker dan Compose plugin aktif
- [ ] Dua database PostgreSQL tersedia
- [ ] `.env.docker` memiliki permission `600`
- [ ] `docker compose config --quiet` berhasil
- [ ] `provision` selesai dengan exit code `0`
- [ ] API `/readyz` mengembalikan `ready: true`
- [ ] Seluruh 15 connector berstatus installed
- [ ] Worker dan UI running
- [ ] Firewall tidak membuka port `3000`/`8080`
- [ ] Akses UI melalui SSH tunnel atau reverse proxy aman
- [ ] Backup awal dibuat
