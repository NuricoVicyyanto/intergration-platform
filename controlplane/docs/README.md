# Dokumentasi Control Plane

Mulai dari halaman ini untuk menemukan panduan yang sesuai.

## Menjalankan aplikasi

- [Authentication dan role](authentication.md) - login, seeder superadmin, dan
  pembatasan akses.
- [Docker local step by step](docker-local-step-by-step.md) - instalasi,
  provisioning, verifikasi, dan troubleshooting di komputer local.
- [Deployment server umum](docker-server-deployment.md) - Linux, akses aman,
  backup, update, dan rollback.
- [Deployment Oracle Linux](docker-oracle-linux-server.md) - khusus Oracle
  Linux 8/9 dengan `dnf`, firewalld, dan SELinux.
- [Ringkasan Docker](../docker/README.md) - service, volume, dan perintah dasar.

## Menggunakan control plane

- [Tipe connection](connection-types.md) - field dan contoh untuk setiap
  connector.
- [Memilih tabel dan memfilter baris](source-selection-dan-row-filter.md) -
  konfigurasi `streams` dan `row_filter`.
- [Menambah connector](menambah-konektor.md) - cara menambah plugin atau driver
  baru.
- [Contoh copy schema PostgreSQL](copy-postgres-schema-gateway-log.md) -
  replikasi tabel dari schema `gateway_log`.

## Operasional

Script deployment berada di [`../deploy/`](../deploy/):

| Script | Fungsi |
|---|---|
| `deploy.sh` | Build, start, menunggu provisioning, dan health check |
| `check.sh` | Memeriksa API, worker, UI, dan koneksi database |
| `backup.sh` | Backup dua database dan dua volume Meltano |
