#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

docker info >/dev/null 2>&1 || fail "Docker daemon tidak dapat diakses"
[[ -f .env.docker ]] || fail ".env.docker tidak ditemukan"
docker compose config --quiet || fail "konfigurasi Compose tidak valid"

docker compose ps --status running --services | grep -qx api ||
  fail "service api tidak berjalan"
docker compose ps --status running --services | grep -qx worker ||
  fail "service worker tidak berjalan"
docker compose ps --status running --services | grep -qx ui ||
  fail "service ui tidak berjalan"

docker compose exec -T api python -c \
  "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/readyz', timeout=3)" \
  >/dev/null || fail "endpoint /readyz API tidak sehat"

docker compose exec -T api python -c \
  "import json, urllib.request; print(json.dumps(json.load(urllib.request.urlopen('http://127.0.0.1:8080/v1/version')), indent=2))"

docker compose exec -T ui wget -q -O /dev/null http://127.0.0.1/ ||
  fail "UI tidak merespons di dalam container"

echo
echo "OK: api, worker, ui, database check, dan UI proxy sehat."
