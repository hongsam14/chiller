# Code Structure

## Build System
- **Type**: Go modules (`go 1.25.7`), **vendored** (`vendor/`, built with `-mod=vendor`). Module path `github.com/STONE-GPY/coolant`.
- **Orchestration**: `Makefile` (build/test/docker/ECR/version), multi-stage Dockerfiles (`Coolant.Dockerfile` worker, `Cooler.Dockerfile` manager — CGO enabled for sqlite; manager image bundles `kubectl`).
- **Code generation**: `//go:generate` via `go tool` — `swag` (Swagger docs from `httpapi/endpoint.go`) and `stringer` (`state.State`). Driven by `make gen`.
- **Local/dev**: `skaffold.yaml` (manager) + `skaffold-worker.yaml`; Helm chart under `infra/coolant`.
- **Versioning**: `VERSION` file (semver, currently drives image tags `v<VERSION>`, `v<VERSION>-<githash>`, `latest`).

## Module Hierarchy

```
cmd/
  cooler/main.go            Manager entrypoint (OTel → Mongo → Manager → gin API)
  coolant/main.go           Worker entrypoint (OTel → Mongo → nuclei RunScan)
  gen_temp_worker_data/     Dev helper: generate test NucleiJobData
  gen_uuid/                 Dev helper: generate a UUID

internal/
  domain/                   Shared types & persistence contracts
    data.go                 NucleiRequest, NucleiJobData, NucleiStatus, StatsMetrics
    config/                 env-driven Config, OtelConfig, RetryMode
    db/                     Mongo client (TLS/DocumentDB), Database + Repository
    state/                  API-facing State enum (+ stringer)
    task/                   Task FSM entity + Repository interface + Mongo impl
  manager/                  Control plane
    manager_k8s.go          Manager: NewTask/Start/Stop/status/progress, hooks, callback
    interfaces.go           TaskManager abstraction over go-task-manager
    options.go              functional options (httpClient, callbackMap)
    handler/                asynq worker body + K8s Job runner
    httpapi/                gin REST server + models + swagger docs
    template/               nuclei-compatible filter, cluster, deps, merge
  core/                     Data plane (worker)
    engine/                 nuclei Executer, RunScan, classifier, progress ticker
    writer/                 Mongo output/log writers, file & slog writers
  opentelemetry/otel.go     OTel SDK bootstrap
  util/util.go              env loading, internal/external IP, base64

infra/                      Helm chart + deploy scripts (core/database/observer)
build/                      dependency/docker download scripts
docs/reports/               task-identity-analysis.md (design rationale)
```

## Existing Files Inventory (modification candidates)

**Entrypoints**
- `cmd/cooler/main.go` — Manager bootstrap & graceful shutdown.
- `cmd/coolant/main.go` — Worker bootstrap; loads `nuclei-data.json`, merges template dirs, runs scan.

**Manager**
- `internal/manager/manager_k8s.go` — Task lifecycle, asynq hooks, resume leftover, Offen callback. **Core orchestration logic.**
- `internal/manager/interfaces.go` — `TaskManager` interface (asynq abstraction).
- `internal/manager/options.go` — functional options.
- `internal/manager/handler/handler_k8s.go` — `HandleNucleiTask`: stale guard, filter, cluster, concurrent Job spawn.
- `internal/manager/handler/k8s_runner.go` — `K8sJobRunner`: build Job from template, mount ConfigMap, RunAndWait.
- `internal/manager/handler/interfaces.go` — `TaskStateRepo`, `JobRunner` interfaces.
- `internal/manager/httpapi/endpoint.go` — all HTTP handlers + auth.
- `internal/manager/httpapi/model.go` — response DTOs.
- `internal/manager/httpapi/interfaces.go` — `TaskService` interface.
- `internal/manager/template/store.go` — `Store.Filter` (nuclei SDK-based).
- `internal/manager/template/cluster.go` — `BuildClusters`, cluster key/dep accessors.
- `internal/manager/template/deps.go` — dependency & payload-file collection, S3-key mapping.
- `internal/manager/template/merge.go` — multi-custom-dir merge (hardlink/copy).
- `internal/manager/template/path.go` — paths→text helper.

**Worker / core**
- `internal/core/engine/engine.go` — `Executer`, `CallEngine`, `RunScan` (two-phase).
- `internal/core/engine/classifier.go` — `ClassifyTemplateIDs`, `DiagnoseExclusions`.
- `internal/core/engine/progress.go` — `OtelStatisticsTicker` (nuclei stats → OTel).
- `internal/core/engine/data.go` — engine `Request` type.
- `internal/core/writer/mongo_output_writer.go` — results/failures/trace/excluded writer (temp-file buffered).
- `internal/core/writer/mongo_log_writer.go` — nuclei log → slog → Mongo.
- `internal/core/writer/slog_writer.go`, `file.go` — gologger bridges.

**Domain**
- `internal/domain/data.go`, `config/config.go`, `config/retry_mode.go`.
- `internal/domain/task/task.go` (entity + Repository interface), `task/mongo_repository.go` (guarded CAS writes), `task/state.go`.
- `internal/domain/state/state.go` (+ `state_string.go` generated).
- `internal/domain/db/mongo.go`, `mongo_db.go`, `mongo_repository.go`.

**Cross-cutting**
- `internal/opentelemetry/otel.go`, `internal/util/util.go`.

## Design Patterns

### Repository pattern
- **Location**: `internal/domain/db`, `internal/domain/task`.
- **Purpose**: Decouple business logic from MongoDB; enable test fakes.
- **Implementation**: `db.Repository` (generic collection ops) + `task.Repository` (Task-specific, guarded writes). Handler/httpapi depend on narrowed interfaces (`TaskStateRepo`, `TaskService`).

### Compare-and-set / optimistic concurrency (`current_asynq_id` guard)
- **Location**: `internal/domain/task/mongo_repository.go`, `handler_k8s.go`, `manager_k8s.go`.
- **Purpose**: Correctness across concurrent asynq retries, resume, stop, and manager restarts. Every mutating write is filtered by `current_asynq_id == asynqID` so stale-attempt writes silently no-op. Terminal transitions compress per-template slices into `*_total` counters in one aggregation-pipeline update.
- **Rationale**: Documented in `docs/reports/task-identity-analysis.md` — Task identity decoupled from asynq attempt identity.

### Functional options
- **Location**: `internal/manager/options.go` (`WithHTTPClient`, `WithCallbackTargets`).

### Adapter / bridge
- **Location**: `writer/slog_writer.go`, `writer/mongo_log_writer.go` (gologger→slog→Mongo); `manager/interfaces.go` (`TaskManager` over `go-task-manager`).

### Two-phase strategy (regular vs DAST)
- **Location**: `engine.RunScan`, `template.BuildClusters`. nuclei can only run one fuzzing mode per process, so templates are partitioned and executed/clustered by mode.

### Singleton (Mongo client)
- **Location**: `internal/domain/db/mongo.go` — `sync.Once`-guarded global client.

## Critical Dependencies
### github.com/projectdiscovery/nuclei/v3 v3.5.1
- **Usage**: Core scan engine (worker) AND template filtering/classification (manager, via SDK dry-run). Deeply embedded — its `output.Writer`, `TemplateFilters`, `types.Options`, catalog/config globals are all used directly.
### github.com/STONE-GPY/go-task-manager v0.3.2
- **Usage**: asynq wrapper + Kubernetes Job `executor` (template Job, ConfigMap projection, RunAndWait). Provides the transport the Manager builds on.
### github.com/hibiken/asynq v0.26.0
- **Usage**: Redis/Valkey-backed task queue (retry, hooks, inspector).
### go.mongodb.org/mongo-driver v1.17.6
- **Usage**: All persistence; aggregation-pipeline updates for atomic state compression.
### k8s.io/api, k8s.io/client-go v0.35.1
- **Usage**: Job/ConfigMap creation and health checks.
### OpenTelemetry (otel v1.40.0 + exporters/contrib)
- **Usage**: Traces, metrics, logs across both binaries; slog bridged via otelslog.
