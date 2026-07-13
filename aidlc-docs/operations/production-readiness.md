# Production Readiness — chiller

**Updated 2026-07-13 (persistence 증분)**. 이전: platform infra 증분(2026-07-11).

**Stage**: OPERATIONS (placeholder in this AI-DLC workflow — no deployment build is executed here).
**Date**: 2026-07-11. Captures the handoff checklist for the platform U1 seam-swap increment (Mongo + Valkey backends, introspection UI, deploy assets).

The code is **green** (build/vet/gofmt/arch-check/`go test ./...`/`-race`). Below is what MUST happen before the durable backends run in production, and the honest limitations carried forward.

## Mandatory before production (blocking)

| # | Action | Why | Where |
|---|---|---|---|
| 1 | ~~**Run the Docker-backed integration suite**~~ — ✅ **DONE 2026-07-13**. `make test-integration` 실행 → **22/22 패키지 ok, FAIL 0** (실 Mongo/Valkey 컨테이너). **다만 CI 파이프라인 편입은 여전히 필요하다.** 실행 결과 **결함 3건이 나왔고 모두 수정됐다**: ① Makefile 타깃이 `./internal/platform/...`만 돌려 durable FindingsStore/ResponseCache는 **CI에서도 실행되지 않았을 것** (→`./...`) ② **Valkey `WorkQueue.Drain`이 동작하지 않았다** — read-ahead된 항목이 drain을 우회해 배달됨(`StopScan`이 중단을 의미하지 않게 됨). mem과 동작이 달랐던 계약 위반 (→`Dequeue`에서 재확인) ③ 계약 스위트가 `Enqueue` 에러를 버리고 `Consume`을 켜지 않아 25분 행(hang) | `internal/{platform/{taskstore,workqueue},findings/store,scan/fetch}/*_test.go` (`//go:build integration`) |
| 2 | **Bump the Go toolchain to ≥ 1.26.5 and rebuild** | `govulncheck` flagged **GO-2026-5856** (crypto/tls ECH privacy leak) on our call paths (proxy/fetch/Mongo/Valkey TLS). Fixed in go1.26.5. No source change needed. | build pipeline / Dockerfile base |
| 3 | **Replace every image tag with a digest** (`@sha256:…`); no `latest` | SECURITY-10/13 supply-chain integrity | `Dockerfile`, `deploy/k8s/*.yaml`, `deploy/compose/docker-compose.yml` |
| 4 | **Provision secrets out-of-band** (Mongo URI w/ `tls=true&w=majority`, Valkey addr+password, API token, storageState) via sealed-secrets/vault | SECURITY-12 — never commit | `deploy/k8s/secret.example.yaml` is a template only |
| 5 | **Enforce TLS + auth on Mongo and Valkey**; enable Mongo replica set (3-node, majority) and Valkey AOF+noeviction | SECURITY-01/09, Q6=B/Q12=B durability | backend config |
| 6 | **Wire CI**: `go vet` · `gofmt` · `make arch-check` · `go test ./...` · `make vuln` · `make test-integration` (Docker job) · `make sbom` · image build w/ digest | RESILIENCY-04, SECURITY-10 | new pipeline |

## Recommended (non-blocking)
- Set the worker HPA to a **KEDA ScaledObject on Valkey stream length** (the manifest ships a portable CPU-based placeholder).
- Add resource requests/limits, PodDisruptionBudgets, and topology-spread constraints per AZ to the k8s manifests.
- Configure Mongo automated backups (oplog + daily snapshot, 7-day retention, encrypted) + a quarterly test-restore (RESILIENCY-12).
- Alerting on: auth failures, `/healthz` degraded, queue depth (consumer-group lag) growth, reaper reclaim rate, Mongo replication lag (RESILIENCY-05/07/14).
- **(persistence 증분)** `/api/introspect/persistence` 카운터에 경보: `cacheErrors`(fail-open 뒤에 숨은 죽은 캐시) · **`cacheHits`가 0에 수렴**(워커들이 서로 다른 Valkey-cache를 가리킴 = FR-02 무력화) · `cacheWipeErrors`/`ledgerPurgeErrors`(사본이 계속 쌓임) · `findingsSaveFailures`.
- **(persistence 증분)** Valkey-**cache** 인스턴스 운영: `maxmemory` + `allkeys-lru`, **복제 불요**(유실 허용, 재fetch로 복구). 큐 인스턴스와 **반드시 분리** — 한 인스턴스에 두면 캐시 LRU가 큐 항목을 evict한다.
- **(persistence 증분)** `CHILLER_EVIDENCE_KEY` 회전 절차 리허설: `"k2:<new>,k1:<old>"`(각 키 id 필수). Mongo/Valkey에 봉인된 데이터는 구 키 없이 열 수 없으므로, 회전 시 구 키를 즉시 폐기하지 말 것.

## Known limitations carried forward (honest — from code review + design)
1. ~~Split-topology mid-run recovery is not automatic.~~ **RESOLVED 2026-07-11 (ledger-lease increment).** Items carry a lease (owner + expiry, heartbeat-refreshed); a lease-safe periodic sweep (manager + all, `ReclaimExpired`) reclaims only expired-lease inflight items, so a dead worker's items are recovered mid-run without clobbering a live worker's. The dashboard scan list (`/api/introspect/scans`) is also live. Safe-reclaim invariant (at most one recorded outcome per item) proven by a stateful PBT. See `construction/platform/code/lease-business-logic-summary.md`.
2. **Probe adapters are stubs** (`argv` nil) — real CLI binaries + parsers are infra-plan §3.
3. **Not in this increment** (infra-plan #3–#4): ProgressReporter→OTel, Authorizer→OIDC/mTLS. (**#5 ResponseCache→Valkey · #6 FindingsStore→Mongo는 persistence 증분에서 DONE 2026-07-13** — 실 컨테이너 통합 테스트 통과, evidence/캐시 값/원장 evidence at-rest 암호화, FR-12 wipe 3경로 배선. 상세 `construction/persistence/`.)
4. **Browser transport hardening** (infra-plan §2): Chromium provisioning, HTTPS CONNECT + MITM CA, session auto-refresh.

## Suggested next tasks (pick one)
- **Probe binaries + real parsers** (infra-plan §3) — 현재 probe 어댑터는 `argv` nil 스텁. **커버리지 충실도 이득이 가장 큼.** data-census 규칙 O-3의 첫 적용 대상(임시 파일 싱크 S12를 census에 먼저 등재).
- **CI 파이프라인 구성** — `go vet` · `gofmt` · `make arch-check` · `go test ./...` · `make vuln` · **`make test-integration`(Docker 잡 — 이번에 처음 실행됐고 결함 3건이 나온 만큼 상시 실행 필수)** · `make sbom` · 다이제스트 빌드.
- **infra-plan #3 ProgressReporter→OTel** — `/api/introspect/persistence`의 카운터를 그대로 읽어가도록 이미 설계됨.
- **infra-plan #4 Authorizer→OIDC/mTLS** — 현재 static Bearer.
