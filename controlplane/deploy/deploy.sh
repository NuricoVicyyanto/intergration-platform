#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

if ! command -v docker >/dev/null 2>&1; then
  echo "ERROR: docker tidak ditemukan." >&2
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo "ERROR: Docker Compose plugin tidak tersedia." >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "ERROR: Docker daemon tidak berjalan atau tidak dapat diakses." >&2
  exit 1
fi

if [[ ! -f .env.docker ]]; then
  echo "ERROR: .env.docker belum ada." >&2
  echo "Jalankan: cp docker.env.example .env.docker" >&2
  exit 1
fi

docker compose config --quiet

echo "[1/4] Build image..."
docker compose build

echo "[2/4] Menjalankan service..."
docker compose up -d --remove-orphans

echo "[3/4] Memeriksa provisioning tenant..."
provision_id="$(docker compose ps -aq provision)"
if [[ -z "${provision_id}" ]]; then
  echo "ERROR: container provision tidak ditemukan." >&2
  docker compose logs --tail=200 provision >&2
  exit 1
fi

provision_state="$(docker inspect -f '{{.State.Status}}' "${provision_id}")"
provision_rc="$(docker inspect -f '{{.State.ExitCode}}' "${provision_id}")"
if [[ "${provision_state}" != "exited" || "${provision_rc}" -ne 0 ]]; then
  echo "ERROR: provisioning berstatus ${provision_state}, exit code ${provision_rc}." >&2
  docker compose logs --tail=200 provision >&2
  exit 1
fi

echo "[4/4] Menunggu API siap..."
api_ready=false
for _ in $(seq 1 60); do
  if docker compose exec -T api python -c \
    "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/readyz', timeout=3)" \
    >/dev/null 2>&1; then
    api_ready=true
    break
  fi
  sleep 3
done

if [[ "${api_ready}" != "true" ]]; then
  echo "ERROR: API belum siap setelah 180 detik." >&2
  docker compose logs --tail=200 api >&2
  exit 1
fi

for service in api worker ui; do
  if ! docker compose ps --status running --services | grep -qx "${service}"; then
    echo "ERROR: service ${service} tidak berjalan." >&2
    docker compose ps >&2
    docker compose logs --tail=100 "${service}" >&2
    exit 1
  fi
done

echo
echo "Deployment selesai."
docker compose ps
echo
echo "UI local/server : http://127.0.0.1:3000"
echo "API local/server: http://127.0.0.1:8080"
