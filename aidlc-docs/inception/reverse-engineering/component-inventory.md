# Component Inventory

## Application Packages
- `cmd/cooler` — Manager entrypoint (REST API + orchestrator).
- `cmd/coolant` — Worker entrypoint (nuclei scan engine).
- `internal/manager` — Task lifecycle, asynq transport, hooks, Offen callback.
- `internal/manager/handler` — asynq worker body + K8s Job runner.
- `internal/manager/httpapi` — gin REST server, DTOs, swagger docs.
- `internal/manager/template` — nuclei-compatible filter, cluster, deps, merge.
- `internal/core/engine` — worker nuclei Executer, classifier, progress ticker.
- `internal/core/writer` — Mongo/output/log/file writers.

## Shared Packages (Models / Utilities / Clients)
- `internal/domain` — `NucleiRequest`, `NucleiJobData`, `NucleiStatus`, `StatsMetrics` (Models).
- `internal/domain/config` — env-driven Config + OtelConfig + RetryMode (Utilities).
- `internal/domain/task` — Task FSM entity + Repository (Models + Client).
- `internal/domain/state` — API State enum (Models).
- `internal/domain/db` — MongoDB access layer (Client).
- `internal/util` — env, IP, base64 helpers (Utilities).

## Infrastructure Packages
- `internal/opentelemetry` — OTel SDK bootstrap (traces/metrics/logs).
- `infra/coolant` — Helm chart: core (manager/worker/queue/templates-storage), database (mongo/valkey/minio/juicefs), observer (grafana/loki/mimir/tempo/otel).
- `infra/*.sh`, `deploy.sh`, `destroy.sh`, `skaffold*.yaml` — deploy tooling.
- `build/` — dependency/docker download scripts.
- Dockerfiles: `Coolant.Dockerfile` (worker), `Cooler.Dockerfile` (manager), `Cooler.Debug.Dockerfile`.

## Dev/Tooling Packages
- `cmd/gen_temp_worker_data` — generate test `NucleiJobData`.
- `cmd/gen_uuid` — generate a UUID.

## Test Packages (co-located `*_test.go`)
- `internal/manager/httpapi/endpoint_test.go` — API handler tests.
- `internal/manager/manager_k8s_test.go` — manager lifecycle tests.
- `internal/manager/template/{cluster,deps,merge,store}_test.go` — template subsystem tests.
- `internal/util/util_test.go` — utility tests.

## External Documentation
- `docs/reports/task-identity-analysis.md` — design rationale for Task-vs-asynq identity decoupling.

## Total Count
- **Application**: 8 packages
- **Shared/Model**: 6 packages
- **Infrastructure/Tooling**: ~5 (opentelemetry + infra chart + build + deploy + docker)
- **Dev helpers**: 2
- **Test files**: 7 (`*_test.go`)
- **Go source files (excl. vendor)**: 47 total (~40 non-test, ~4,900 LOC non-test excl. generated docs)
- **Language**: Go 1.25.7, single module, vendored.
