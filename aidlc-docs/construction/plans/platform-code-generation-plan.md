# Code Generation Plan — Unit `platform` (U1, last unit)

**Stage**: 🟢 CONSTRUCTION → `platform` → Code Generation (Part 1: Planning) · **Date**: 2026-07-08
**Single source of truth for platform code generation.**

**Unit context**
- control plane + **composition root**. Wires the finished data plane (scan/findings) end-to-end. No new judgement logic.
- Spec: `../platform/functional-design/{domain-entities,business-rules,business-logic-model}.md` (E-1..6, PR-1..11).
- **arch-check**: platform MAY import domain + scan + findings (only *reverse* is forbidden). Verified against Makefile.
- **Infra-swappable seams (E-6/PR-11, user-emphasized)**: every store/queue/progress seam method carries `context.Context` first + `error` last; CAS returns `(bool, error)`; entities serializable. MVP ships in-memory impls; Infra adds `*_redis.go`/`*_mongo.go` beside them without touching the interface or orchestrator.

**Reuse discipline**: consumes scan (`exec.Executor.Run(ctx,…)`, `registry`, `ingest`, `safety`, `fetch`) + findings (`normalize/dedup/severity/compliance/coverage/report`) + domain — no reimplementation. **New dep (Q1=B)**: `github.com/playwright-community/playwright-go` for the auth-proxy browser session; otherwise stdlib.

**금융권 인증 transport 추가 (browser-proxy-transport-clarification.md, Q1=B/Q2=A/Q3=B/Q4=A)**:
- 4번째 역할 **`proxy`**: 단일 인증 브라우저 세션(storageState) 보유, worker 트래픽에 세션 주입 후 포워딩(MITM). worker는 `HTTP(S)_PROXY`로 그 proxy 경유 → 단일 세션 불변식 유지하며 수평확장.
- `BrowserSession`(인터페이스 + Playwright 구현 + **fake**) + `AuthProxy`(포워드 프록시) + 탭풀 T(기본 6).
- **scan 유닛 경미 변경**: `fetch.NewFetcher`에 proxy URL 옵션(`Transport.Proxy`), probe 어댑터에 `HTTP_PROXY` 주입(testssl 제외). arch-check 불변.

**Real vs deferred (honest scope)**
- **Real + tested now**: 5 seams(in-memory), Orchestrator FSM + fan-out + **동시성 전략(워커풀 W + 호스트-인터리브 + per-host in-flight 캡)** + worker loop + finalize/fold + resume/stop, boot gate, `net/http` ScanAPI + Bearer authz, **`AuthProxy` + `BrowserSession`(Playwright 구현) + 탭풀 + 세션주입 + 라우팅**, `cmd/chiller` wiring(`--role=all|manager|worker|proxy`), 통합 테스트(BrowserSession fake로 CI 브라우저 불요).
- **Deferred (documented, Infra/build)**: Redis/Valkey `WorkQueue`, Mongo `TaskStore`, OTel exporter, OIDC/mTLS `Authorizer` — 동일 인터페이스. **Chromium 바이너리 provisioning + `playwright install` + MITM CA 발급/배포 + HTTPS 인터셉션 하드닝(스트리밍/WS) + 세션 자동갱신** = build/Infra(OSS CLI 번들과 동일 원칙).

---

## 도메인 계약 추가 (Step 0 — domain 유닛 경미 확장, PR-12c)
- [x] `internal/domain` — `TransportKind` (`ProxyEligible`=기본, `DirectOnly`) + 선택 capability `TransportAware { Transport() TransportKind }`. (앞선 `CheckResult`/`WorkItemOutcome.NotApplicable` 추가와 동일한 커널 확장 패턴; arch-check 불변.)
- [x] `internal/scan/checking/probe` — testssl 어댑터가 `Transport() DirectOnly` 구현; 나머지는 기본(ProxyEligible). observation은 기본.
- [x] 배선이 `Transport()`를 읽어 per-checker proxy 주입 결정(Step 6c/7).

## Package layout
`internal/platform/` (contracts) + subpackages (impls) + `cmd/chiller` (wiring). Subpackages import the root contract package; the root imports domain + findings/report + scan/ingest only; `cmd/chiller` imports everything and injects.

## Steps

### Step 1 — Contracts `[x]` (E-1..6)
- [x] `internal/platform/` (package `platform`): `types.go` (`Scan`, `ScanState`, `ItemState`, `WorkLedgerEntry`, `ProgressSnapshot`, alias `ScanRequest = ingest.ScanRequest`) + `seams.go` (`TaskStore`, `WorkQueue`, `ProgressReporter`, `Authorizer`, `Orchestrator` — exact E-5 signatures with ctx+error). FSM state consts + `ScanState.Terminal()`.

### Step 2 — TaskStore (mem) `[x]` (Q1=A, PR-2/11)
- [x] `internal/platform/taskstore/` — in-memory `TaskStore` (map+RWMutex): SaveScan/LoadScan, `CompareAndSetState`/`CompareAndSetItemState` (atomic under lock), AppendWorkItems, RecordOutcome, `ListUnfinished` (pending∪inflight), AllOutcomes, Save/LoadReport. Values returned by copy (E-6 rule 4). + **contract test** (CAS races, unfinished ledger, idempotent resume).

### Step 3 — WorkQueue (mem) `[x]` (Q1/Q2=A, PR-3a)
- [x] `internal/platform/workqueue/` — buffered-channel `WorkQueue`: Enqueue/`Dequeue(ctx)` (blocks on ctx), `Drain(scanID)`. + test (ctx cancel unblocks, drain drops a scan's items).

### Step 4 — ProgressReporter (mem) `[x]` (Q5=A, US-028)
- [x] `internal/platform/progress/` — in-memory counters: Init/OnClaimed/OnFinished/`Snapshot`. + test.

### Step 5 — ScanAPI + authz `[x]` (Q3/Q4=A, PR-7)
- [x] `internal/platform/api/` — `BearerAuthorizer` (static token, fail-closed 401) + `Handler` (stdlib mux): `POST /scans` (authz→ingest→CreateScan→StartScan→202), `GET /scans/{id}` (Snapshot JSON), `GET /scans/{id}/report` (Report JSON or 202 if running), `GET /healthz`. + test (authz fail-closed, bad request 400, happy path via a fake Orchestrator).

### Step 6 — Orchestrator + fold + boot gate + 동시성 `[x]` (PR-1..6, PR-3a, US-024/025/026/027)
- [x] `internal/platform/orchestrator/`:
  - `orchestrator.go` — `Orchestrator` impl over injected seams + scan Executor + findings pipeline. CreateScan/StartScan(fan-out by `Scope()`, **호스트-인터리브 enqueue** PR-3a)/GetStatus/GetReport.
  - `worker.go` — 워커 풀(W, `--workers` 기본 16) 루프: Dequeue→CAS claim(pending→inflight)→`Executor.Run(ctx,…)`→RecordOutcome→progress; finalize trigger when `ListUnfinished==∅`. per-host in-flight 캡은 `RateLimiter` release 배선(scan safety).
  - `finalize.go` — normalize→dedup→store; **fold** (PR-5: WorkItemOutcome[]→`coverage.ScanOutcome` via `compliance.Mapper.Project(cwe,KISA)` for NotApplicable, RanOK from success-anywhere)→`Accountant.Account`→`report.Build`→SaveReport→CAS(running→completed).
  - `resume.go` / `stop.go` — ResumeScan (re-enqueue unfinished, idempotent), StopScan (CAS→stopped, Drain, ctx cancel).
  - `boot.go` — `BootGate`: builds registry ownership + loads configs + `coverage.New(...)`; returns Accountant or a fatal error (PR-6). 
  - + tests: fan-out counts, worker aggregation, **fold rule** (applicable-ok→Checked / na-only→N/A / fail→ToolFailed), resume (no double-run), stop (ctx cancel), boot gate (missing mapping → refuse).

### Step 6b — AuthProxy + BrowserSession `[x]` (PR-12, Q1=B/Q3=B/Q4=A)
- [x] `internal/platform/proxy/`:
  - `session.go` — `BrowserSession` 인터페이스 + `playwrightSession`(playwright-go v0.5200.1: 1 Browser→1 Context+storageState, **작업 탭풀 T** `--browser-tabs` 기본 6, `context.request` 우선/Page 필요분) + `fakeSession`(테스트).
  - **로그인 유지탭 제외 (사용자 강조)**: 팀원이 개발 중인 로그인 대행 패키지는 로그인 유지 확인을 위해 **탭 1개를 상시 점유**한다. 따라서 동시성 탭풀 `T`는 **작업 전용**이고 로그인 유지탭은 그 풀에서 **제외**한다 — 총 열린 페이지 = T(work) + 1(keep-alive 예약). 작업 세마포어 크기 = 정확히 T. `SessionConfig{ Tabs T; ReserveKeepAlive bool }` + `LoginKeeper` seam(미래 팀원 패키지가 주입, 전용 예약 탭 소유; 지금은 인터페이스+예약 슬롯만, 실제 로그인 로직 없음).
  - `authproxy.go` — `AuthProxy`: 포워드 프록시(HTTP 절대형; HTTPS MITM은 Infra 이연), 수신 요청→`BrowserSession.Do`(세션 주입)→응답. per-host rate(SR-4) 강제. testssl 우회는 상위(라우팅)에서.
  - + 테스트(fakeSession): 세션 주입 확인, **작업 동시성 상한 == T (keep-alive 예약이 있어도 T에서 차감 안 됨)**, testssl 우회, ctx 취소 종료.

### Step 6c — scan 유닛 proxy 연동 (경미) `[x]` (PR-12c/d)
- [x] `internal/scan/fetch/fetch.go` — `NewFetcher`에 선택적 proxy URL → `Transport.Proxy`. + 회귀 테스트(proxy 설정 시 그리로 egress).
- [x] `internal/scan/checking/probe/` — 어댑터에 proxy env 옵션; **testssl `Transport() DirectOnly`**, 나머지 기본. financial 배선이 `Transport()==ProxyEligible`인 probe에만 `HTTP_PROXY` 주입. + 테스트(testssl은 proxy env 미설정, 나머지는 설정). arch-check 불변 확인.

### Step 7 — Composition root `[x]` (PR-8/11b/12)
- [x] `cmd/chiller/main.go` — flags (`--role=all|manager|worker|proxy`, `--addr`, `--token`, config paths, `--store=mem`, `--queue=mem`, `--workers=16`, `--browser-tabs=6`, `--storage-state`, `--proxy-addr`, `--financial`), load configs, assemble checkers, **run BootGate (fail→exit non-zero)**, instantiate mem seams + safety/fetch(+proxy if financial) + findings pipeline, inject Orchestrator, start by role. `proxy` 역할은 BrowserSession+AuthProxy 기동. MVP registers only mem impls.

### Step 8 — Integration tests `[x]` (§8 of business-logic-model)
- [x] `test/platform_e2e_test.go` — end-to-end `--role=all` over an `httptest` target: submit→workers→completed→report has all 26 PS-IDs classified (Validate) + findings present. Plus: resume (unfinished re-run only), fail-isolation, authz fail-closed, boot-gate refusal, stop (ctx cancel), **동시성**(다중 호스트가 단일 워커 대비 벽시계 단축; 느린 호스트가 빠른 호스트 비차단), **financial proxy**(fakeSession: worker가 proxy 경유해 인증 응답 수신, testssl 직접).

### Step 9 — Summary `[x]`
- [x] `aidlc-docs/construction/platform/code/business-logic-summary.md`.

## Verification
`go build ./... · go vet ./... · go test ./... · gofmt · make arch-check` (data plane must not import platform; platform→domain+scan+findings OK).

## Checkboxes
- [x] Step 0 · [x] Step 1 · [x] Step 2 · [x] Step 3 · [x] Step 4 · [x] Step 5 · [x] Step 6 · [x] Step 6b · [x] Step 6c · [x] Step 7 · [x] Step 8 · [x] Step 9

**Status**: Part 2 (code generation) COMPLETE — 2026-07-10. All steps generated + tested. `go build/vet/test/-race`, `gofmt -l` (clean), `make arch-check` (OK) all green. Summary: `../platform/code/business-logic-summary.md`.
