# Technology Stack

## Programming Languages
- **Go** — 1.25.7 — both binaries; CGO enabled (musl) for the sqlite driver dependency.

## Frameworks & Core Libraries
- **gin** (`gin-gonic/gin` v1.11.0) — REST API HTTP framework (manager).
- **nuclei/v3** (`projectdiscovery/nuclei/v3` v3.5.1) — scan engine + template filtering SDK. Central dependency.
- **asynq** (`hibiken/asynq` v0.26.0) — Redis/Valkey task queue (retry, hooks, inspector).
- **go-task-manager** (`STONE-GPY/go-task-manager` v0.3.2) — asynq wrapper + Kubernetes Job executor.
- **mongo-driver** (`go.mongodb.org/mongo-driver` v1.17.6) — MongoDB / DocumentDB access.
- **gologger** (`projectdiscovery/gologger` v1.1.59) — nuclei logging, bridged to slog.
- **swaggo** (`swaggo/swag`, `gin-swagger`, `files`) — Swagger/OpenAPI docs generation.
- **caarlos0/env** v3.5.0 — env-based configuration binding.
- **google/uuid** v1.6.0, **rs/xid** v1.6.0 — identifiers.
- **samber/slog-multi** — slog fan-out (stdout + OTel).
- **golang.org/x/sync** (errgroup) — bounded-concurrency Job spawning.

## Persistence & Messaging
- **MongoDB / AWS DocumentDB** — results, tasks, logs, traces (TLS via RDS CA bundle; `retryWrites=false` for DocumentDB).
- **Redis / Valkey** — asynq queue backend.
- **MinIO / S3** — nuclei template distribution (`nuclei-templates` bucket).
- **NFS PVC** (`coolant-templates`) — shared template volume (read-only) for manager + worker.
- **sqlite / gorm** (`gorm.io/driver/sqlite`, `gorm.io/gorm`) — present as dependency (transitive/optional); drives CGO requirement.

## Infrastructure
- **Kubernetes** (`k8s.io/api`, `client-go`, `apimachinery` v0.35.1) — Job/ConfigMap orchestration.
- **Kueue** (kueue.x-k8s.io/v1beta2) — Worker Job admission/quota (ClusterQueue/LocalQueue/ResourceFlavor).
- **JuiceFS CSI Driver** — cluster storage dependency.
- **Helm** — packaging (`infra/coolant` chart).
- **Docker** — multi-stage builds; images pushed to **AWS ECR** (`ap-northeast-2`).
- **Skaffold** — local dev inner loop.

## Observability
- **OpenTelemetry** (`go.opentelemetry.io/otel` v1.40.0 + otlp gRPC exporters for traces/metrics/logs; otelgin, otelmongo, otelslog bridges).
- **Grafana / Loki / Mimir / Tempo** — observability backend (Helm `observer` namespace); **OTel Collector** as the ingest point.
- **Interactsh** (`projectdiscovery/interactsh`) — nuclei OOB interaction server.

## Build Tools
- **Go modules** (vendored) + **Make** (build/test/gen/ECR/version) + **Docker** multi-stage.
- **go:generate tools**: `swaggo/swag/cmd/swag`, `golang.org/x/tools/cmd/stringer` (declared in `go.mod` `tool` block).

## Testing Tools
- **stretchr/testify** v1.11.1 — assertions/mocks.
- **alicebob/miniredis/v2** v2.37.0 — in-memory Redis for asynq/queue tests.
