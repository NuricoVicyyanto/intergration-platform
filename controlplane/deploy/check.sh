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

docker compose exec -T api python - <<'PY'
import json
import os
import urllib.parse
import urllib.request


def get(path):
    with urllib.request.urlopen(f"http://127.0.0.1:8080{path}", timeout=10) as response:
        return json.load(response)


tenants = get("/v1/tenants").get("data", [])
if not tenants:
    raise SystemExit("FAIL: belum ada tenant terdaftar")

bootstrap_tenant = os.environ.get("CP_BOOTSTRAP_TENANT", "acme")
tenant = next(
    (item for item in tenants if item.get("name") == bootstrap_tenant),
    tenants[0],
)

install_all = os.environ.get(
    "CP_BOOTSTRAP_INSTALL_ALL_CONNECTORS", "true"
).strip().lower() != "false"

query = urllib.parse.urlencode({"tenant_id": tenant["id"]})
connectors = get(f"/v1/connectors?{query}").get("data", [])
installed = [item for item in connectors if item.get("installed")]
print(
    f"Connector tenant {tenant['name']}: "
    f"{len(installed)}/{len(connectors)} installed"
)
if install_all and len(installed) != len(connectors):
    missing = ", ".join(
        item["name"] for item in connectors if not item.get("installed")
    )
    raise SystemExit(f"FAIL: connector belum terpasang: {missing}")
PY

echo
echo "OK: api, worker, ui, database, dan connector sehat."
