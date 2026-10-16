-- Skema control plane.
-- Dibuat agar cocok dengan rancangan di meltano-go-reference/13-arsitektur-control-plane.md bagian 13.4.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Satu tenant = satu direktori project Meltano.
CREATE TABLE IF NOT EXISTS tenants (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name             text NOT NULL UNIQUE,
  display_name     text,
  environment_name text NOT NULL,
  project_dir      text NOT NULL,
  created_at       timestamptz NOT NULL DEFAULT now()
);

-- Koneksi: kredensial TIDAK disimpan di sini, hanya referensi ke secret manager.
CREATE TABLE IF NOT EXISTS connections (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name       text NOT NULL,
  kind       text NOT NULL CHECK (kind IN ('source', 'destination')),
  driver     text NOT NULL CHECK (driver IN ('postgres', 'mysql', 'mssql', 'oracle')),
  endpoint   jsonb NOT NULL DEFAULT '{}'::jsonb,
  username   text,
  secret_ref text NOT NULL,
  options    jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS pipelines (
  id                         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id                  uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name                       text NOT NULL,
  extractor                  text NOT NULL,
  loader                     text NOT NULL,
  source_connection_id       uuid REFERENCES connections(id),
  destination_connection_id  uuid REFERENCES connections(id),
  state_id_suffix            text,
  default_timeout_seconds    integer NOT NULL DEFAULT 3600 CHECK (default_timeout_seconds > 0),
  max_attempts               integer NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
  enabled                    boolean NOT NULL DEFAULT true,
  created_at                 timestamptz NOT NULL DEFAULT now(),
  updated_at                 timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, name),
  -- state_id_suffix tidak boleh mengandung ':' (lihat doc 14.1.6)
  CONSTRAINT pipelines_suffix_no_colon CHECK (state_id_suffix IS NULL OR position(':' in state_id_suffix) = 0)
);

CREATE TABLE IF NOT EXISTS runs (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  pipeline_id      uuid NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
  status           text NOT NULL
                   CHECK (status IN ('queued','running','cancelling','succeeded','failed','cancelled','timed_out','lost')),
  attempt          integer NOT NULL DEFAULT 1,
  auto_attempts    integer NOT NULL DEFAULT 0,
  retried_from     uuid REFERENCES runs(id),
  trigger          text NOT NULL DEFAULT 'manual',
  reason           text,

  -- korelasi ke sisi Meltano
  meltano_state_id text,
  meltano_run_id   uuid,

  -- hasil terminal
  exit_code        integer,
  error            text,
  log_tail         text,

  -- lease
  claimed_by       text,
  lease_expires_at timestamptz,

  -- idempotency
  idempotency_key  text,

  created_at       timestamptz NOT NULL DEFAULT now(),
  started_at       timestamptz,
  ended_at         timestamptz,

  -- hasil pemetaan --timeout
  timeout_seconds  integer NOT NULL DEFAULT 3600,
  full_refresh     boolean NOT NULL DEFAULT false,
  state_id_suffix  text
);

-- Satu pipeline hanya boleh punya satu run aktif. Ini yang menggantikan
-- pengaman advisory Meltano (JobFinder.latest_running).
CREATE UNIQUE INDEX IF NOT EXISTS ux_one_active_run_per_pipeline
  ON runs (pipeline_id)
  WHERE status IN ('queued', 'running', 'cancelling');

CREATE UNIQUE INDEX IF NOT EXISTS ux_runs_idempotency
  ON runs (idempotency_key)
  WHERE idempotency_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS ix_runs_claim  ON runs (status, created_at) WHERE status = 'queued';
CREATE INDEX IF NOT EXISTS ix_runs_pipeline ON runs (pipeline_id, created_at DESC);
CREATE INDEX IF NOT EXISTS ix_runs_lease  ON runs (lease_expires_at) WHERE status IN ('running','cancelling');

-- Klaim atomik. Ganti :worker dan :lease_seconds saat dipakai dari kode.
--   UPDATE runs SET status='running', claimed_by=$1,
--          lease_expires_at = now() + ($2 || ' seconds')::interval, started_at = now()
--    WHERE id = (SELECT id FROM runs WHERE status='queued'
--                 ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1)
-- RETURNING *;

CREATE TABLE IF NOT EXISTS run_events (
  id       bigserial PRIMARY KEY,
  run_id   uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  at       timestamptz NOT NULL DEFAULT now(),
  kind     text NOT NULL,
  payload  jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS ix_run_events_run ON run_events (run_id, at);
