# Integration Test Instructions — chiller

## Purpose
Prove the four units cohere end to end: the control plane (`platform`) correctly wires the data plane (`scan` + `findings`) so a submitted scan flows submit → fan-out → workers → fold → 26-item coverage + report. All integration tests use **in-memory seams** and a `FakeSession`, so **no external services and no Chromium are required**.

Location: `test/` package (`coverage_wiring_test.go`, `platform_e2e_test.go`).

## Test Scenarios

### Scenario 1: Cross-unit coverage wiring (`scan` ownership → `findings` boot gate)
- **Description**: real `configs/mappings/kisa.yaml` + `configs/coverage/kisa.yaml` + checker CWE-ownership must partition exactly the **26 KISA items** (weakness ∪ attacker ∪ delegation) — findings' boot completeness check.
- **Setup**: none (loads production config from `configs/`).
- **Expected**: `TestCoverageWiring_RealConfig_BootPasses` green; PS-025 Checked (attacker axis), PS-014→ASM, PS-005→Nuclei delegated.

### Scenario 2: End-to-end submit → report (`platform` → `scan` → `findings`)
- **Description**: submit one URL against an `httptest` target, workers run every checker, finalize folds a **valid 26-partition** and builds a report with a real finding (sensitive HTML comment → PS-009).
- **Setup**: in-process orchestrator + worker pool (`newHarness`).
- **Expected**: `TestE2E_SubmitToReport_26Partition` — `Coverage.Validate(spec)` passes, 26 items, a finding present.

### Scenario 3: Fault isolation
- **Description**: a checker that always errors marks only its PS-ID `ToolFailed`; the scan still completes and every other item is accounted (US-027).
- **Expected**: `TestE2E_FailIsolation` — PS-003 = ToolFailed, coverage still valid.

### Scenario 4: Worker-pool concurrency (PR-3a)
- **Description**: 4 server-delayed URLs complete in well under the serial sum, proving the pool parallelizes.
- **Expected**: `TestE2E_WorkerPoolConcurrency` — elapsed < 350ms (serial ≈ 480ms).

### Scenario 5: Financial auth-proxy (PR-12)
- **Description**: in financial mode, worker egress routes through the `AuthProxy` backed by a `FakeSession`; the target observes the injected authenticated session header.
- **Expected**: `TestE2E_FinancialProxy_InjectsSession` — target sees `X-Auth-Session`.

### Scenario 6: Resume + Stop (FSM)
- **Expected**: `TestE2E_Resume_Idempotent` (no double-run), `TestE2E_Stop_Transitions` (running→stopped, queue drained).

## Setup Integration Test Environment
### 1. Start Required Services
```bash
# None. httptest spins up in-process target/proxy servers; seams are in-memory.
```
### 2. Configure Service Endpoints
```bash
# None. The harness injects everything.
```

## Run Integration Tests
### 1. Execute Integration Suite
```bash
go test ./test/...                 # all integration scenarios
go test -race -count=1 ./test/...  # race + bypass cache (recommended for timing scenarios)
```
### 2. Verify Service Interactions
- **Expected Results**: all scenarios pass; the 26-item partition validates against the production spec.
- **Logs Location**: stdout; add `-v` for per-scenario detail.

### 3. Cleanup
```bash
# None. httptest servers close via t.Cleanup/defer; no external state persists.
```

## Deferred to Infra (documented, not in the MVP integration suite)
- Redis/Valkey `WorkQueue`, Mongo `TaskStore`, OTel exporter, OIDC/mTLS `Authorizer` — same interfaces; contract tests exist at the unit level.
- Real Chromium `BrowserSession` end-to-end (HTTPS CONNECT + MITM CA) — a Build/Infra integration concern with `playwright install`.
- `manager`/`worker` role split e2e — requires the durable shared queue (Redis), an Infra path; the `all` role is the MVP integration path.

---

# Tier 2 — Durable Backend Contract Tests (added 2026-07-11, infra increment)

## Purpose
Prove the **Mongo `TaskStore`** and **Valkey `WorkQueue`** reproduce the *identical observable behavior* as the in-memory impls (PR-11a). They run the **same contract suite**, parameterized over the constructor — a backend swap that changes behavior fails here.

## Prerequisites
- **Docker daemon reachable by the test process** (testcontainers-go pulls `mongo:7` and `redis:7-alpine`).
- No manual service startup: the tests start and terminate the containers themselves.

## Run
```bash
make test-integration
# = go test -tags integration ./internal/platform/...
```

## What it covers
| Suite | Backend | Asserts |
|---|---|---|
| `TestMongo_TaskStoreContract` | Mongo container | CAS single-winner (32 concurrent claimers → exactly 1), state-CAS guard, unfinished ledger ∪ outcomes, resume-does-not-reclaim-done, `ListScansByState`, PBT serialization round-trip (real BSON — this is where the `bson` tags + string enums are actually exercised), PBT ledger command-sequence vs model oracle |
| `TestValkey_WorkQueueContract` | Valkey/Redis container | FIFO for a single consumer, Dequeue blocks until enqueue, ctx-cancel unblocks, Drain never dispatches a drained scan's items, PBT enqueue/dequeue multiset |
| `TestValkey_StatsReflectsDepth` | Valkey container | `QueueInspector.Stats` reports live depth/pending |

## Expected result
All sub-tests pass. A failure here means the durable backend diverges from the seam contract — **do not deploy that backend**.

## Cleanup
Automatic (`testcontainers.TerminateContainer` in `t.Cleanup`).

## ⚠️ Current status
**Compiles clean (`go vet -tags integration` OK) but has NOT been executed** — the authoring environment had no Docker daemon available to the test run. **This suite must be executed in a Docker-capable CI job before the durable backends are trusted in production.**
