# Code Generation Summary — platform (U1) Infrastructure Increment

**Scope**: `TaskStore`→Mongo, `WorkQueue`→Valkey(Streams) seam-swap + introspection UI + deploy assets. Generated 2026-07-11. **Green** (build/vet/gofmt/arch-check/test/-race).
**Principle honored**: `platform.TaskStore`/`platform.WorkQueue` interfaces **unchanged** (PR-11); orchestrator/scan/proxy code untouched. Selection is entirely in `cmd/chiller` (PR-11b).

## What was built

### Serialization (Q4=A — resolves F-3/F-4)
- `bson` tags added to persisted types: `domain.{WorkItem,WorkItemOutcome,Target}`, `ingest.{ScanRequest,ScanOptions}`, `platform.{Scan,WorkLedgerEntry}`. Tags are strings — **no new imports in domain/scan**, so `make arch-check` stays green.
- `ScanState`/`ItemState` persist as **strings** via `MarshalBSONValue`/`UnmarshalBSONValue` (isolated in `internal/platform/types_bson.go`) + `ParseScanState`/`ParseItemState`. int-iota drift (F-4) eliminated.
- `Scan.SchemaVersion` (=1) added as a future migration branch point. `WorkLedgerEntry` gained `ID`/`ScanID` so the ledger document has a stable `_id`.

### TaskStore → Mongo (`internal/platform/taskstore/taskstore_mongo.go`)
- `NewMongo(ctx, uri, db)`: majority write-concern (Q12=B), connectivity Ping (fail-closed, SECURITY-15), index creation. `Close`/`Ping` (non-seam) for shutdown + health.
- Collections `scans` / `workledger` / `reports` (Q5=A, separate ledger docs). Indexes `{state:1}`, `{scanId:1,state:1}`.
- **CAS** = `findOneAndUpdate` with expected-state filter → single-winner (PR-4). No-match returns `(false, nil)` (matches mem contract).
- `var _ platform.TaskStore = (*Mongo)(nil)`.

### WorkQueue → Valkey Streams (`internal/platform/workqueue/workqueue_valkey.go`)
- Single shared stream `chiller:queue:stream` + consumer group `workers` (Q2=B resident pool model). `NewValkey(ctx, addr, consumer, Options)`.
- **C2=A, seam unchanged**: internal **reader** goroutine `XREADGROUP`s and hands items to `Dequeue`, then `XACK`s (item left the queue = a worker took it); internal **reaper** goroutine `XAUTOCLAIM`s a crashed process's un-acked entries and redelivers them; correctness guaranteed by the ledger CAS regardless of redelivery (E-6 rule 5). No Ack method added to the seam.
- **Drain lazy** (shared stream can't be per-scan `DEL`'d): `SADD chiller:queue:drained`; the reader skips+XACKs drained items so they are never dispatched.
- `var _ platform.WorkQueue = (*Valkey)(nil)`. `Ping`/`Close` (non-seam).

### QueueInspector + dashboard (user request: "(1) + UI")
- **Separate optional interface** `platform.QueueInspector{Stats}` (NOT part of WorkQueue → PR-11 intact). Implemented by both mem (depth/drained) and Valkey (`XLEN`/`XPENDING`/`XINFO`/`SCARD` + reaper reclaim counter).
- `internal/platform/api/introspect.go`: read-only `GET /api/introspect/queue` (authz'd, SECURITY-08) + static `GET /ui` dashboard (dependency-free HTML/JS, no CDN). Scan lookup reuses the existing `/scans/{id}`.
- **SECURITY-04 now applies** (HTML served): CSP/HSTS/X-Content-Type-Options/X-Frame-Options=DENY/Referrer-Policy on `/ui`.
- Deep **`/healthz`** (RESILIENCY-06) pings registered backends, fail-closed 503; shallow `/livez`. Wired via `api.WithHealthChecks` + `api.WithIntrospection` options (base `api.New(authz,orch)` signature preserved).

### Composition root (`cmd/chiller`, F-5/PR-11b)
- Hard guard removed → `buildBackends` selects `mem|mongo` / `mem|valkey`. New flags `--mongo-uri/--mongo-db/--valkey-addr`; secrets prefer env (`CHILLER_MONGO_URI`, `CHILLER_VALKEY_ADDR`, `CHILLER_VALKEY_PASSWORD`, SECURITY-12). mem remains default (backward compatible). Backends' health probes + Close wired.

### Tests
- **Contract suites extracted** (F-2): `taskstore/contract_test.go`, `workqueue/contract_test.go` — parameterized over `newStore`/`newQueue`; old white-box tests removed. Mem runs them by default.
- **PBT** (project's math/rand style, consistent with findings unit — not rapid): round-trip `Unmarshal(Marshal)==x` (PBT-02), ledger command-sequence vs in-test model oracle (PBT-05/06), queue multiset/FIFO. Seeds logged (PBT-08).
- **Integration** (`//go:build integration`, Q10=A): `taskstore_mongo_test.go` + `workqueue_valkey_test.go` run the SAME contract suites against real Mongo/Valkey via testcontainers-go. `make test-integration`.

### Deploy + CI
- `Dockerfile` (multi-stage, distroless nonroot, digest-pin note), `deploy/compose/docker-compose.yml` (Mongo + 2 Valkey, Q9=C), `deploy/k8s/` (manager 1, worker Deployment+HPA, proxy singleton 1, Mongo StatefulSet 3, Valkey queue, Secret example, NetworkPolicy) + README.
- `Makefile`: `test-integration`, `vuln` (govulncheck), `sbom` (syft) targets.

## Dependencies added (SECURITY-10, pinned in go.mod/go.sum)
`go.mongodb.org/mongo-driver v1.17.9` · `github.com/redis/go-redis/v9 v9.20.1` · `github.com/testcontainers/testcontainers-go v0.37.0` (+ mongodb/redis modules, test-only).

## Verification (green)
`go build ./...` · `go vet ./...` · `gofmt -l` clean · `make arch-check` OK · `go test ./...` all pass (contracts + PBT + UI/health + existing e2e) · `go test -race ./internal/platform/... ./test/` clean. Integration suite compiles under `-tags integration` (runs with Docker).

## Honestly deferred
- Integration suite requires Docker at run time (not executed in this environment; compiles clean).
- Mongo/Valkey **TLS + auth** are enforced via URI/Options + deploy config, not by the Go code (SECURITY-01 at the connection layer).
- k8s manifests are a **reference skeleton** — resource limits, PDBs, topology-spread, and the concrete KEDA XLEN scaler are finalized with the cluster in Operations.
- Still out of this increment (infra-plan): ProgressReporter→OTel (#3), Authorizer→OIDC/mTLS (#4), ResponseCache→Valkey (#5), FindingsStore→Mongo (#6). The shared-instance placement for #5/#6 is pre-decided in `shared-infrastructure.md`.
