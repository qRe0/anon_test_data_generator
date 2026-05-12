# AGENTS.md — anon_test_data_generator

## What this is
A Go CLI that generates synthetic, referentially-integer test data for PostgreSQL
while guaranteeing GDPR/PCI-DSS/HIPAA compliance. Not started yet — only design docs exist.

## Reference docs
- `docs/implementation_plan.md` — authoritative spec: tech stack, directory layout, module plan, implementation order
- `docs/diploma.md` — full thesis: algorithms, compliance analysis, architecture rationale (Russian)

## Tech stack (from implementation plan)
| Component           | Library               |
|---------------------|-----------------------|
| Config              | `gopkg.in/yaml.v3`    |
| DB driver           | `pgx/v5`              |
| Fake data           | `gofakeit/v7`         |
| PII detection       | Microsoft Presidio (external Python microservice) |
| Containerization    | Docker + docker-compose |
| CI                  | GitHub Actions        |

## Directory layout (DO NOT DEVIATE)
```
/cmd            — entrypoint (main.go)
/internal
  /config       — YAML loader + validator
  /schema       — DB introspection (pg_catalog / information_schema)
  /pii          — HTTP client for Presidio API
  /graph        — dependency graph, Kahn's algorithm, ExecutionPlan
  /generator    — value generators (factory + registry), ID pool, worker pool
  /exporter     — buffered COPY via pgx, cleanup strategies
  /validator    — post-generation SQL smoke tests
/configs        — example YAML configs
/docker         — Dockerfile, docker-compose.yml
/scripts        — helpers
```

## Implementation order (MVP: stages 2–8)
1. `go mod init`, scaffold directories, add deps, create `Makefile`
2. Config module (`/internal/config`) — loader + validator with tests
3. Schema introspection (`/internal/schema`) — extract tables, columns, PK, FK, constraints
4. Dependency graph (`/internal/graph`) — Kahn's algorithm, cycle detection, ExecutionPlan
5. Generator registry (`/internal/generator`) — factory pattern, gofakeit providers, ID pool
6. Worker pool (`/internal/generator`) — producer-consumer with channels, batching, graceful shutdown
7. Exporter (`/internal/exporter`) — COPY protocol via pgx, truncate/delete cleanup
8. PII client (`/internal/pii`) — Presidio integration (medium priority)
9. Post-generation validator, integration tests, Docker/GitHub Actions (last)

## Key architectural rules
- **Modular monolith** — no microservices for the core engine (IPC overhead is unacceptable for ETL)
- **Hybrid PII detection** — Presidio is called ONCE at startup to classify columns, then cached. No runtime ML calls.
- **Pipeline pattern**: Analyzer → Resolver (topo sort) → Dispatcher → Workers → Batch Writer → PostgreSQL
- **Determinism**: fixed seed fed to `rand.New(rand.NewSource(seed))`, never `math/rand` default source
- **Referential integrity**: Kahn's algorithm orders tables; Reservoir Sampling ID pools (cap ~10k) link FKs
- **COPY protocol only** for bulk insert — `pgx.CopyFrom()`, never row-by-row INSERT
- **Batch + time-based flush**: accumulate to `batch_size` rows OR flush every 500ms via `time.Ticker`
- **Graceful shutdown**: `context.Context` for cancellation, `sync.WaitGroup` for workers, final flush

## Testing
- Unit tests for every module (table-driven)
- Integration tests: real PostgreSQL via docker-compose (users + orders schema, FK links)
- Determinism test: two runs with same seed → identical output
- Performance: target > 10,000 RPS; memory should not grow linearly (check with pprof)
