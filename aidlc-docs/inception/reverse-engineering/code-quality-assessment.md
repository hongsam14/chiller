# Code Quality Assessment

## Test Coverage
- **Overall**: Fair — focused on the highest-risk correctness surfaces, not broad.
- **Unit Tests**: Present (7 `*_test.go` files) covering the concurrency-critical and logic-dense areas:
  - `internal/manager/manager_k8s_test.go` — Task lifecycle / asynq hook behavior.
  - `internal/manager/httpapi/endpoint_test.go` — API handlers.
  - `internal/manager/template/{cluster,deps,merge,store}_test.go` — template filtering/clustering/merge/deps.
  - `internal/util/util_test.go`.
- **Gaps**: No dedicated tests for `internal/core/engine` (`RunScan`, `ClassifyTemplateIDs`, `DiagnoseExclusions`), the `writer` package (Mongo output/flush), `task/mongo_repository.go` guarded CAS writes, or `handler/k8s_runner.go`. The two-phase RunScan and the `current_asynq_id` guard invariants are the riskiest untested/under-tested logic.
- **Integration/E2E**: None automated in-repo; validation relies on `allow_direct_run` debug Jobs and manual K8s deployment.

## Code Quality Indicators
- **Linting**: No `.golangci.yml` / lint config committed. `go vet`-level hygiene appears maintained by convention.
- **CI/CD**: No `.github/` workflows or pipeline config in-repo. Build/test/release are driven manually via `Makefile` (`make test`, `make build`, `make ecr-push`).
- **Code Style**: Consistent and idiomatic Go — narrowed interfaces at seams, functional options, context propagation everywhere, structured slog logging with module labels, `ErrorContext`/`WarnContext` used consistently.
- **Documentation**: Strong. Package-level and function-level doc comments are unusually thorough, especially around the concurrency invariants (I1–I4) in `task`, `manager`, and `handler`. `docs/reports/task-identity-analysis.md` records the design rationale. README documents versioning, ECR, and the breaking schema change.

## Technical Debt
- **Commented-out / dead code**: `buildTemplateFilter` (httpapi), `RunJob`/`getJobName`/`getConfigMapName` (single-URL path superseded by clustered path), disabled `RequestStatsLog`/`WriteStoreDebugData` stubs, commented callback-auth block in `cmd/cooler/main.go`.
- **Global mutable state**: `nuclei.DefaultConfig` / `config.DefaultConfig` are process globals; `template.Store.Filter` takes an exclusive mutex around `SetTemplatesDir` to compensate — correct but fragile. Package-level `otelProgress` var in `engine` is reassigned per scan.
- **`templatesWaitGroup`/`templatesLock` + `updateTemplates()`** in `manager_k8s.go` are declared but `updateTemplates` appears unused from the current call graph — candidate dead code.
- **Error handling asymmetry**: worker `RunScan` intentionally swallows per-phase errors ("continuing"), so a fully-failed scan can still report success at the engine level; task-level success/failure is instead driven by the handler/hook path. This is deliberate but subtle.
- **CGO dependency**: sqlite/gorm pull in CGO, complicating cross-compilation (Dockerfiles add musl-dev). Worth confirming sqlite is actually needed at runtime.
- **Large vendored tree**: ~450 indirect modules (nuclei ecosystem) — large attack/supply-chain surface, slow builds, no committed license scan.

## Patterns and Anti-patterns
- **Good Patterns**:
  - Compare-and-set concurrency via `current_asynq_id` guard on every mutating write; terminal transitions atomically compress slices→counters in one aggregation-pipeline update (avoids doc bloat and TOCTOU windows).
  - Clean control-plane / data-plane split; components communicate only via ConfigMap payload + shared Mongo (no fragile RPC).
  - Interface-based seams enabling test fakes (`TaskManager`, `TaskService`, `TaskStateRepo`, `JobRunner`, repositories).
  - nuclei-SDK-based filtering (rather than reimplementing) keeps manager-side filtering 100% consistent with the worker's engine.
  - Resume correctness: leftover computed from expanded − completed, already-completed clusters skipped.
- **Anti-patterns / risks**:
  - Reliance on process-global nuclei config guarded only by a mutex (`template/store.go`) — safe today, but any future concurrent use outside the lock would corrupt filtering.
  - Handler swallows individual cluster errors into `jobErrors` then returns a combined error for asynq retry — retry re-runs the whole payload (already-completed clusters are skipped via the completed-set, mitigating cost, but partial-failure semantics are intricate).
  - No CI/lint gate means regressions in the delicate FSM invariants depend on developers running `make test` manually.

## Recommendations (for later Requirements/Design stages)
1. Add tests for `engine.RunScan` two-phase behavior, `ClassifyTemplateIDs`, and `task` repository guard invariants.
2. Introduce a CI pipeline (build + `go test ./...` + `golangci-lint`) and a license/vulnerability scan (govulncheck) given the vendored surface.
3. Remove confirmed dead code (`updateTemplates`, single-URL `RunJob` path, commented blocks).
4. Verify the sqlite/gorm/CGO requirement; drop it if unused to simplify builds.
