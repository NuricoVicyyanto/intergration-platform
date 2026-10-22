# Authentication dan Role

Dashboard dan seluruh endpoint data dilindungi cookie session HttpOnly.
Endpoint publik hanya `/healthz`, `/readyz`, `/v1/version`, dan login.

## Akun superadmin pertama

Seeder berjalan saat API start dan hanya membuat user jika tabel user masih
kosong. Nilai default Docker:

```text
Username: superadmin
Password: SuperAdmin!2026-Meltano
```

Ubah password setelah login pertama melalui menu **Password** di sidebar.
Seeder tidak menimpa password pada restart berikutnya.

Konfigurasi dapat diganti sebelum start pertama:

```dotenv
CP_SUPERADMIN_USERNAME=superadmin
CP_SUPERADMIN_DISPLAY_NAME=Super Admin
CP_SUPERADMIN_PASSWORD=GANTI_PASSWORD_MINIMAL_12_KARAKTER
CP_SESSION_HOURS=24
```

Jika user sudah pernah dibuat, perubahan `CP_SUPERADMIN_PASSWORD` tidak
mengubah password lama.

## Role

| Role | Dashboard | Pipeline/connection | Kelola user |
|---|---|---|---|
| `superadmin` | Ya | Ya | Ya |
| `admin` | Ya | Ya | Tidak |

Superadmin dapat membuat admin lain melalui halaman **Users**. Password minimal
12 karakter. Session disimpan sebagai hash token di database dan dicabut ketika
user dihapus, password diganti, atau logout.

## Endpoint

| Method | Path | Akses |
|---|---|---|
| `POST` | `/v1/auth/login` | Publik |
| `POST` | `/v1/auth/logout` | Login |
| `GET` | `/v1/auth/me` | Login |
| `POST` | `/v1/auth/password` | Login |
| `GET` | `/v1/users` | Superadmin |
| `POST` | `/v1/users` | Superadmin |
| `PATCH` | `/v1/users/{id}` | Superadmin |
| `DELETE` | `/v1/users/{id}` | Superadmin |

Semua endpoint tenant, connection, pipeline, dan run juga memerlukan session.

Collection Postman untuk kontrak login tersedia di
[`../postman/auth.postman_collection.json`](../postman/auth.postman_collection.json).

## Update server

```bash
cd /opt/meltano-controlplane
git pull --ff-only
bash controlplane/deploy/deploy.sh
bash controlplane/deploy/check.sh
```

Migration `0008_add_auth.sql` berjalan otomatis. Pada deployment pertama
setelah update, API membuat akun superadmin dari environment.
