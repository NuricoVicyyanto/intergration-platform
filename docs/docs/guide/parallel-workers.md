---
title: Run Pipelines in Parallel Workers
description: Run independent Meltano jobs concurrently on one host or in a container.
layout: doc
sidebar_position: 10
---

`meltano run` executes the blocks in one invocation in series. To run independent
jobs concurrently, use [`meltano run-many`](/reference/command-line-interface#run-many).
It starts a separate `meltano run` process for each job and limits how many
processes can run at once.

This process-based design is intentional. It keeps project configuration,
database sessions, signal handlers, and plugin subprocesses isolated between
pipelines.

## Requirements

- Each argument to `run-many` must be the name of an independent Meltano job.
- Every job gets a unique state ID suffix. The default suffix is `{job}`.
- Pipelines that write to the same target table still need to be coordinated at
  the destination. Running them concurrently does not remove warehouse locks.
- Plugins must be installed before workers start.
- Use PostgreSQL for Meltano's system database in production. SQLite is suitable
  for a local demonstration but can become a write bottleneck.

## Run locally without Docker

From the Meltano project directory:

```bash
meltano install
meltano --environment=prod run-many --workers=3 \
  customers-sync orders-sync invoices-sync
```

If five jobs are supplied with `--workers=3`, three jobs start immediately. The
remaining jobs start when workers become available. A failing job does not stop
the other jobs, and the command exits non-zero after reporting every result.

Preview the isolated child commands without running them:

```bash
meltano --environment=prod run-many --workers=3 --dry-run \
  customers-sync orders-sync invoices-sync
```

## Run locally with Docker

The repository includes a source-build Dockerfile and a dedicated Compose file:

- `docker/meltano/Dockerfile.source` builds Meltano from the checked-out source.
- `docker-compose.workers.yml` starts a local PostgreSQL metadata database,
  installs project plugins, and runs `meltano run-many` with three workers.

Create a local configuration file:

```bash
cp docker/workers.env.example docker/workers.env
```

Edit `docker/workers.env` so `MELTANO_PROJECT_DIR` points to the Meltano project
containing `meltano.yml`, and `MELTANO_JOBS` contains the independent job names.

Build the image:

```bash
docker compose \
  --env-file docker/workers.env \
  -f docker-compose.workers.yml \
  build
```

Run the jobs:

```bash
docker compose \
  --env-file docker/workers.env \
  -f docker-compose.workers.yml \
  up \
  --abort-on-container-exit \
  --exit-code-from workers
```

The worker installs project plugins into the persistent `meltano-runtime` volume
before starting parallel pipelines. Pipeline logs and metadata are preserved in
Docker volumes between runs.

## Deploy updates on a server

The image is built from source, so a server update is:

```bash
git pull
docker compose \
  --env-file docker/workers.env \
  -f docker-compose.workers.yml \
  build
```

Then start a pipeline batch using the same `up` command shown above. To build a
versioned image, set `MELTANO_WORKER_IMAGE` in `docker/workers.env`, for example:

```dotenv
MELTANO_WORKER_IMAGE=registry.example.com/data/meltano-workers:2026-10-23
```

If the same server should run the latest source after every deployment, add the
`build` and `up` commands to the server's deployment script or CI/CD workflow.
Keep `docker/workers.env` on the server because it contains environment-specific
paths, job names, and database configuration.

## Schedule recurring batches

`run-many` processes a batch and exits. Use an external scheduler such as cron,
Airflow, systemd timers, or a Kubernetes CronJob to start a new batch. Do not run
the same Docker Compose command every minute while a previous batch is still
running; the scheduler should allow only one batch at a time.

For example, a server cron entry could start a production batch hourly. Make sure
the repository path, environment file, logs, and lock strategy are appropriate
for the server before enabling the schedule.

## Configuration reference

| Variable | Default | Purpose |
| --- | --- | --- |
| `MELTANO_PROJECT_DIR` | Required | Host path to the Meltano project |
| `MELTANO_ENVIRONMENT` | `prod` | Meltano environment used by every worker |
| `MELTANO_JOBS` | Required | Space-separated independent job names |
| `MELTANO_RUN_MANY_WORKERS` | `3` | Maximum concurrent worker processes |
| `MELTANO_RUN_MANY_STATE_ID_SUFFIX_TEMPLATE` | `{job}` | Per-job state ID suffix |
| `MELTANO_DATABASE_URI` | Local Compose PostgreSQL | Meltano system database |
| `MELTANO_WORKER_IMAGE` | `meltano-workers:local` | Built image tag |
| `MELTANO_PYTHON_VERSION` | `3.13` | Python base image version |

## Troubleshooting

### Plugin not found

The Compose entrypoint runs `meltano install` before starting workers. If running
`meltano run-many` directly, run `meltano install` first. Workers deliberately use
`--no-install` to avoid concurrent plugin installation.

### Database is locked

The local Compose setup already uses PostgreSQL for Meltano metadata. If SQLite
is used elsewhere, move to PostgreSQL before increasing worker concurrency.

### Pipelines wait for each other

Check the destination for table locks or use separate target schemas. Also verify
that each job has a distinct state ID suffix and that the source APIs do not
enforce a lower concurrency limit.

### Workers stop when the terminal closes

Use a scheduler or a supervised deployment for recurring production runs. The
Compose workflow shown here is a foreground batch runner, not a long-lived
scheduler.

### Docker build stalls or reports no space left

The full source image includes build tools and database drivers. Keep several
gigabytes free on the Docker host. Check disk usage with `docker system df` and
remove unused build cache with `docker builder prune` before retrying the build.
