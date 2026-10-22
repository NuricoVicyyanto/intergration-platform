#!/bin/sh
set -eu

tenant="${CP_BOOTSTRAP_TENANT:-acme}"
environment="${CP_BOOTSTRAP_ENVIRONMENT:-dev}"
extractor="${CP_BOOTSTRAP_EXTRACTOR:-tap-postgres}"
loader="${CP_BOOTSTRAP_LOADER:-target-postgres}"
install_all="${CP_BOOTSTRAP_INSTALL_ALL_CONNECTORS:-false}"
tenants_root="${CP_TENANTS_ROOT:-/srv/tenants}"
project_dir="${tenants_root}/${tenant}"
marker="${project_dir}/.docker-provisioned"

mkdir -p "${tenants_root}"

if [ "${install_all}" = "true" ]; then
  provision_signature="catalog-v1"
else
  provision_signature="custom:${extractor}:${loader}"
fi

current_signature=""
if [ -f "${marker}" ]; then
  current_signature="$(cat "${marker}")"
fi

if [ "${current_signature}" = "${provision_signature}" ]; then
  echo "Tenant ${tenant} sudah diprovision; melewati instalasi plugin."
elif [ "${install_all}" = "true" ]; then
  echo "Menginstal seluruh connector katalog untuk tenant ${tenant}."
  echo "Langkah ini dapat memerlukan waktu lama dan bandwidth besar."

  while read -r tap target; do
    echo "Menyiapkan ${tap} -> ${target}"
    /usr/local/bin/provision \
      -tenant "${tenant}" \
      -extractor "${tap}" \
      -loader "${target}"
  done <<'CONNECTORS'
tap-postgres target-postgres
tap-mysql target-mysql
tap-mssql target-mssql
tap-oracle target-oracle
tap-mongodb target-mongodb
tap-rest-api-msdk target-s3
tap-csv target-duckdb
tap-google-sheets target-duckdb
CONNECTORS

  printf '%s\n' "${provision_signature}" > "${marker}"
else
  echo "Menyiapkan tenant ${tenant}: ${extractor} -> ${loader}"
  echo "Langkah ini mengunduh dependency plugin dan dapat memerlukan beberapa menit."

  /usr/local/bin/provision \
    -tenant "${tenant}" \
    -extractor "${extractor}" \
    -loader "${loader}"

  printf '%s\n' "${provision_signature}" > "${marker}"
fi

echo "Mendaftarkan tenant ${tenant} ke control plane..."
python - "${tenant}" "${environment}" <<'PY'
import json
import sys
import urllib.error
import urllib.request

tenant = sys.argv[1]
environment = sys.argv[2]
base = "http://api:8080"

with urllib.request.urlopen(f"{base}/v1/tenants", timeout=10) as response:
    tenants = json.load(response).get("data", [])

if any(item.get("name") == tenant for item in tenants):
    print(f"Tenant {tenant} sudah terdaftar.")
    raise SystemExit(0)

body = json.dumps({
    "name": tenant,
    "display_name": tenant,
    "environment_name": environment,
}).encode()
request = urllib.request.Request(
    f"{base}/v1/tenants",
    data=body,
    headers={"Content-Type": "application/json"},
    method="POST",
)
try:
    with urllib.request.urlopen(request, timeout=10) as response:
        created = json.load(response)
except urllib.error.HTTPError as error:
    if error.code == 409:
        print(f"Tenant {tenant} sudah terdaftar.")
        raise SystemExit(0)
    raise

print(f"Tenant {created['name']} terdaftar dengan id {created['id']}.")
PY

echo "Tenant ${tenant} siap digunakan."
