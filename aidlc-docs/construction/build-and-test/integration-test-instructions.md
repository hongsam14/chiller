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

---

## r3 추가 시나리오 (persistence 증분) — Docker 필요, `-tags integration`

> **실행**: `make test-integration` (testcontainers가 Mongo/Valkey 컨테이너를 코드에서 직접 띄운다 — 사전 `docker-compose up` 불요)
> **⚠️ 현재 상태**: 아래 4개 스위트는 **컴파일·vet만 검증됐고 실행된 적이 없다** (작성 환경에 Docker 없음). `production-readiness.md` blocking #1.

### Scenario 6: FindingsStore 계약 (Mongo) — mem과 동일 스위트
- **파일**: `internal/findings/store/store_mongo_test.go` (`TestMongo_FindingsStoreContract`)
- **검증**: 라운드트립 · **순서 보존**(Mongo는 정렬 없이 문서 순서를 보장하지 않으므로 `idx` 정렬이 실제로 동작하는지) ·
  멱등 재저장 · **꼬리 삭제**(이전 실행이 더 많은 finding을 남겼을 때 유령 제거) · **evidence 삭제 금지** · 스캔 격리
- **왜 중요한가**: 순서가 어긋나면 `FrameworkLabels`가 엉뚱한 finding을 가리킨다(조용한 컴플라이언스 오염)

### Scenario 7: at-rest 암호화 — **raw 바이트로 단언**
- **파일**: `store_mongo_test.go` (`TestMongo_EvidenceIsEncryptedAtRest`) · `taskstore_mongo_test.go`
  (`TestMongo_LedgerEvidenceIsEncryptedAtRest`) · `cache_valkey_test.go` (`TestValkey_CachedResponseIsEncryptedAtRest`)
- **검증**: seam을 통과하면 평문이 나오지만, **Mongo의 raw BSON / Valkey의 raw 값에는 평문이 없다**
  (evidence snippet · 응답 본문 · **대상 사이트 세션 쿠키**). 동시에 인덱스 대상 필드(targetUrl)는 **평문으로 남아** 있어야 한다
- **왜 이렇게 테스트하는가**: 라운드트립 테스트는 암호화 여부를 **증명하지 못한다** — 평문 저장도 통과한다

### Scenario 8: TTL 인덱스 정합 (`--findings-ttl`)
- **파일**: `store_mongo_test.go` (`TestMongo_TTLIndexOnlyWhenAsked`)
- **검증**: 기본값(0)에서는 TTL 인덱스가 **생성되지 않는다**(Q7=C 무기한 보존) · `>0`이면 생성된다 ·
  값을 바꿔 재기동하면 `IndexOptionsConflict` 없이 **drop 후 재생성**된다 · 0으로 되돌리면 인덱스가 **제거**된다

### Scenario 9: ResponseCache 계약 + wipe (Valkey)
- **파일**: `internal/scan/fetch/cache_valkey_test.go`
- **검증**: 계약 스위트 전체 · **모든 키에 TTL이 걸려 있다**(프로세스가 죽어 wipe를 못 해도 만료로 사라짐) ·
  **`WipeScan` 후 `chiller:cache:{scanId}:*` 키가 0개** · 인덱스 SET이 evict돼도 SCAN 폴백으로 지운다
- **cleanup**: testcontainers가 자동 종료 (`t.Cleanup`)

### Scenario 10 (권장, 미구현): 캐시 백엔드 다운 시 fail-open
- **검증**: Valkey-cache 컨테이너를 스캔 도중 정지 → 스캔이 **완주**하고(느려질 뿐), `cacheErrors` 카운터가 증가하며,
  재fetch가 **per-host rate budget을 여전히 소비**한다 (RESILIENCY-14 카오스)
- **현재**: 유닛 레벨(`TestFetcher_CacheFailsOpen`)로만 검증됨. 컨테이너 정지 시나리오는 CI에서 추가할 것
