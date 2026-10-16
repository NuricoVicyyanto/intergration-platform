> Bagian 9 dari 16 - [Daftar Isi](README.md)

## 9. Struktur Direktori Proyek Go yang Direkomendasikan

```
replicator/
├── cmd/
│   ├── control-plane/main.go          # API + scheduler entry point
│   └── worker/main.go                 # worker entry point
├── internal/
│   ├── api/                           # net/http handlers, middleware, DTO
│   │   ├── router.go
│   │   ├── jobs.go
│   │   ├── connections.go
│   │   └── middleware.go
│   ├── domain/                        # tipe inti, tanpa dependency infrastruktur
│   │   ├── job.go                     # Job, JobSpec, JobStatus
│   │   ├── state.go                   # Checkpoint, StateVersion
│   │   ├── schema.go                  # Stream, Table, Column, TypeMapping
│   │   └── errors.go                  # TransientError / PermanentError
│   ├── connector/
│   │   ├── registry.go                # map[Kind]Factory, capability
│   │   ├── source.go                  # interface Source
│   │   ├── sink.go                    # interface Sink
│   │   ├── postgres/                  # implementasi bertahap
│   │   ├── mysql/
│   │   ├── mssql/
│   │   └── oracle/
│   ├── replication/
│   │   ├── pipeline.go                # orkestrasi source -> engine -> sink
│   │   ├── checkpoint.go              # CheckpointPolicy
│   │   ├── backpressure.go            # bounded channel + flow control
│   │   └── transform.go               # mapping/typing antar dialect
│   ├── state/
│   │   ├── store.go                   # interface StateStore
│   │   ├── postgres.go
│   │   ├── objectstore.go             # S3/GCS/Azure (opsional)
│   │   └── memory.go                  # untuk test
│   ├── queue/
│   │   ├── queue.go                   # interface Queue
│   │   ├── redis.go
│   │   └── postgres.go                # SKIP LOCKED
│   ├── worker/
│   │   ├── worker.go                  # loop klaim job + eksekusi
│   │   ├── executor.go
│   │   └── heartbeat.go
│   ├── resilience/
│   │   ├── retry.go                   # backoff + klasifikasi error
│   │   └── breaker.go
│   ├── observability/
│   │   ├── logging.go                 # slog JSON + konteks job_id/run_id
│   │   ├── metrics.go                 # prometheus
│   │   └── tracing.go                 # otel
│   ├── config/
│   │   └── config.go                  # env + file + secret, berlapis
│   └── store/
│       ├── migrations/                # SQL/sqlc migrations
│       └── queries/                   # sqlc query definitions
├── pkg/
│   └── singerv1/                      # opsional: codec Singer-compatible
├── api/openapi.yaml
├── deploy/                            # Dockerfile, compose, helm
├── testdata/                          # fixture source/destination
├── go.mod
└── Makefile
```

Catatan pemetaan: `internal/connector/registry.go` <-> `src/meltano/core/plugin/factory.py`;
`internal/state/store.go` <-> `src/meltano/core/state_store/base.py`;
`internal/replication/pipeline.go` <-> `src/meltano/core/block/extract_load.py`;
`internal/worker/heartbeat.go` <-> `src/meltano/core/job/job.py`.

---

**Navigasi:** [Bagian 8](08-pemetaan-ke-golang.md) - [Daftar Isi](README.md) - [Bagian 10](10-roadmap-implementasi.md)
