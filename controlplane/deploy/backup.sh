#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

if [[ ! -f .env.docker ]]; then
  echo "ERROR: .env.docker tidak ditemukan." >&2
  exit 1
fi

umask 077
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup_dir="${BACKUP_DIR:-${ROOT_DIR}/backups/${timestamp}}"
mkdir -p "${backup_dir}"

postgres_image="${POSTGRES_CLIENT_IMAGE:-postgres:16-alpine}"

echo "Membuat backup database ke ${backup_dir}..."
docker run --rm \
  --env-file .env.docker \
  -v "${backup_dir}":/backup \
  "${postgres_image}" \
  sh -ec '
    pg_dump --format=custom --file=/backup/controlplane.dump "$CP_DATABASE_URL"
    pg_dump --format=custom --file=/backup/meltano-system.dump "$CP_SYSTEM_DB_URI"
  '

echo "Membuat backup volume tenant..."
docker run --rm \
  -v meltano-controlplane_tenants:/source:ro \
  -v "${backup_dir}":/backup \
  alpine \
  tar -czf /backup/tenants.tar.gz -C /source .

echo "Membuat backup volume Meltano home..."
docker run --rm \
  -v meltano-controlplane_meltano-home:/source:ro \
  -v "${backup_dir}":/backup \
  alpine \
  tar -czf /backup/meltano-home.tar.gz -C /source .

echo "Backup selesai:"
ls -lh "${backup_dir}"
