# Functional Design — Unit `platform` (U1) · Business Rules

**Stage**: 🟢 CONSTRUCTION → `platform` → Functional Design · **Date**: 2026-07-08

Rules PR-1..PR-10. Platform은 판정하지 않고 **수명주기·분배·재개·집계·엣지**를 규율합니다.

---

## FSM 전이표 (PR-1, US-024)

| # | From | Event | To | 사전조건 | 사후조건 / CAS |
|---|---|---|---|---|---|
| T1 | `created` | `StartScan` | `running` | Scan 영속됨 | fan-out(PR-3) → WorkLedger 채움 → 전원 enqueue; `CAS(created→running)` |
| T2 | `running` | 모든 WorkItem 종료 | `completed` | `ListUnfinished == ∅` | 회계(PR-6)+리포트 조립 → `Report` 저장; `CAS(running→completed)` |
| T3 | `running` | `StopScan` | `stopped` | — | 미분배 item 취소, inflight는 자연 종료; `CAS(running→stopped)` |
| T4 | `created` | `StopScan` | `stopped` | — | `CAS(created→stopped)` |
| T5 | `running` | `ResumeScan` | `running` | 미완 item 존재 | `ListUnfinished` 재큐만; **재진입**(상태 불변, 멱등) |
| T6 | `running` | 치명 오류 | `failed` | 저장소/큐 복구불가 | `CAS(running→failed)` |

- **PR-2 (원자성)**: 모든 상태 전이는 `TaskStore.CompareAndSetState(id, expected, new)`. 기대 상태 불일치 → 전이 거부(동시 요청 안전). 종료 상태(`completed`/`stopped`/`failed`)에서 나가는 전이 없음.
- **멱등 재개(PR-2a)**: `ResumeScan`은 `running→running` 재진입 — 여러 번 호출해도 완료 item 재실행 없음(원장이 진실원천).

## Fan-out (PR-3, US-026, C-6)

- `StartScan`은 등록된 체커를 `Scope()`로 분류:
  - **URL-scoped**: 각 in-scope URL마다 WorkItem 1개 = (그 URL × 모든 URL-scoped 체커, 공유 fetch 1회 — FR-02).
  - **host-scoped**: 각 (host × host-scoped 체커) 쌍마다 WorkItem 1개.
- **PR-3a (동시성 전략 — 자산 스캔 시간 단축)**: 병렬 단위는 WorkItem.
  - **워커 풀 W** (`--workers`, 기본 16; 체커는 CPU-경량이고 지연은 fetch I/O라 코어보다 큼) — 자산이 많을수록 W까지 near-linear 단축. 큐는 bounded 버퍼로 backpressure.
  - **per-host token bucket(SR-4)이 intra-host 상한** — 단일 호스트에 URL이 많으면 워커가 아니라 rate가 상한(의도된 politeness/self-DoS 방지). cross-host는 W까지, intra-host는 rate-bound.
  - **호스트-인터리브 fan-out** — 아이템을 호스트 라운드로빈으로 enqueue → 워커가 여러 호스트로 분산, 한 호스트의 rate 대기가 풀 전체를 멈추는 HOL(head-of-line) 방지.
  - **per-host in-flight 캡** — `RateLimiter.Acquire`가 반환하는 `release func()`(현 no-op)를 per-host 세마포어로 실배선 → rate 안에서도 한 호스트 커넥션 폭주 방지.
  - **단일 fetch 재사용(ResponseCache)** — URL당 1 GET을 URL-scoped 체커 전부가 공유 → 아웃바운드·시간 ~10배 절감.
  - **financial 모드(PR-12)**: 앱계층 요청이 proxy 탭풀 T를 통과 → 유효 병렬성 `min(W, T, per-host rate)`. 단일 세션 스캔은 **T가 지배 상한**. URL 총량은 `ingest.MaxURLs`(기본 500, H3)로 상한.

## 워커 루프 (PR-4, US-025/027)

- 워커는 `WorkQueue.Dequeue` → `CompareAndSetItemState(pending→inflight)` 성공 시 소유 → `scan.Executor.Run(ctx, item, cache)` → `RecordOutcome(done, outcome)`.
- **PR-4a (context)**: `Run`에 스캔 ctx 전달(H4 수정) — StopScan/타임아웃이 in-flight fetch를 취소.
- **PR-4b (격리, R7)**: 한 WorkItem 실패는 그 item만 `failed` 처리, 스캔 전체를 실패시키지 않음. `WorkItemOutcome.Failed`가 체커별 실패를 운반.
- **PR-4c (단일 fetch 재사용)**: `ResponseCache`는 스캔 스코프. 재개 시 캐시 히트면 재fetch 안 함, 미스면 URL당 정확히 1 GET(FR-02).

## WorkItemOutcome → coverage.ScanOutcome 변환 (PR-5, 회계 입력)

스캔 종료 시 모든 `WorkItemOutcome`를 체커 축으로 접어 `coverage.ScanOutcome` 생성. 각 소유 체커 `c`, 대상 항목의 CWE에 대해, 실행된 모든 WorkItem에 걸쳐:

| 신호 | 조건 | 집계 규칙 |
|---|---|---|
| **applicable-ok** | `c`가 오류 없이 실행 + 그 CWE를 `NotApplicable`에 넣지 **않음** | 어느 한 대상에서라도 발생 → `RanOK[c]=true` |
| **na** | `c`가 실행됐고 그 CWE를 `NotApplicable`에 넣음 | applicable-ok가 **한 번도** 없을 때만 → `NotApplicable[c][psid]=true` |
| **fail** | `c`가 `Failed`에 있음 | applicable-ok/na 둘 다 없으면 → 둘 다 미설정 → `statusFor`가 `ToolFailed` |

- **PR-5a**: CWE→PS-ID 변환은 `compliance.Mapper`(findings) 사용 — platform은 매핑 로직을 재구현하지 않음.
- **PR-5b (N/A 우선순위 주의)**: 한 PS-ID를 여러 체커가 소유하고 하나는 Checked·하나는 N/A면 `coverage.statusFor`의 na-우선으로 N/A가 됨 — MVP 수용(문서화). 실제 26항목은 대부분 1소유자.
- 결과 `ScanOutcome`를 `Accountant.Account`에 투입 → `domain.Coverage` (26분할, `Validate(spec)` 보장).

## 부팅 완전성 게이트 (PR-6, R5, Q5=A)

- 기동 시 `registry.OwnershipForFindings()` + 매핑 파일 + 커버리지 스펙으로 `coverage.New(...)` 호출. **실패(26 항목이 weakness∪attacker∪delegated로 안 덮임) → 프로세스가 스캔을 수락하지 않고 기동 실패(non-zero exit)**. 
- cross-unit wiring 테스트가 검증한 불변식을 **런타임 게이트**로 승격 — 잘못 설정된 배포가 조용히 커버리지 구멍을 내는 것을 차단.
- `/healthz`는 부팅 게이트 통과 + 저장소/큐 reachable 여부 반환.

## 엣지 / authz (PR-7, US-001/023, Q3/Q4=A)

- **PR-7a (표면)**: stdlib `net/http` 3 엔드포인트 — `POST /scans`(제출), `GET /scans/{id}`(상태), `GET /scans/{id}/report`(리포트). 오케스트레이터는 순수 Go 인터페이스, 핸들러는 얇은 어댑터.
- **PR-7b (authz fail-closed)**: 모든 요청은 `Authorizer.Authorize(req)` 통과 필수. MVP=Bearer 정적 토큰. 토큰 없음/불일치 → `401`, 처리 안 함. `safety.ApprovalGate`와 동일 fail-closed 철학.
- **PR-7c (검증)**: 제출 URL/옵션은 `ingest.Ingest`로 검증(재구현 금지); 실패 → `400`.

## 역할 모델 (PR-8) — 4종 [all, manager, worker, proxy]

- `--role=all`(기본): 한 프로세스가 manager(API+FSM+조립) + worker(루프) [+ financial 모드면 proxy도 in-process]. `WorkQueue`=in-process.
- `--role=manager` / `--role=worker`: 분리 기동, 분리 모드 큐는 durable 백엔드 필요(Infra Redis).
- **`--role=proxy`**: 단일 인증 브라우저 세션 프로세스. worker 트래픽 인증 포워딩. MVP in-memory 백엔드에선 `all`이 완전 동작; proxy는 financial 모드에서 별도/in-process 기동.

## 금융권 인증 Transport / Proxy (PR-12, browser-proxy-transport-clarification.md)

- **PR-12a (단일 세션 불변식)**: 한 스캔 전체에서 **인증 BrowserContext는 정확히 1개** — worker 수와 무관. 세션은 `proxy` 프로세스에만 존재. worker는 무상태이며 아웃바운드를 `HTTP(S)_PROXY`로 그 proxy에 보냄. (Q3=B)
- **PR-12b (세션 주입)**: `AuthProxy`는 수신 요청을 `BrowserSession.Do`로 재요청 — 컨텍스트의 최신 쿠키/헤더 주입. storageState는 운영자가 사전 제공(Q2=A). HTTPS는 번들 CA로 MITM(worker가 CA 신뢰).
- **PR-12c (툴 분류는 체커가 데이터로 선언 — 하드코딩 금지)**: 각 체커가 **`domain.TransportKind`** 를 선언한다 — `ProxyEligible`(기본; 앱계층 → financial 모드에서 proxy 경유) 또는 `DirectOnly`(대상 직접만). **testssl=`DirectOnly`**(TLS 핸드셰이크가 점검 대상이라 프록시 무의미), 나머지 probe + observation=`ProxyEligible`.
  - 도메인 계약 추가: `type TransportKind int {ProxyEligible, DirectOnly}` + 선택 capability `interface TransportAware { Transport() TransportKind }`(registry `owned` 패턴). 미선언 체커의 기본값은 **`ProxyEligible`** — financial 모드에서 선언 누락 체커가 미인증으로 대상을 때리는 오답보다 인증 proxy 경유가 안전(fail-safe 방향).
  - 배선(주입 시점)이 `Transport()`를 읽어 결정: 공유 fetch(observation)는 financial이면 proxy(모두 ProxyEligible라 균일); 각 probe는 financial+ProxyEligible이면 `HTTP_PROXY` 주입, DirectOnly면 미주입. 오케스트레이터/executor는 분기 없이 미리 구성된 체커를 실행만 — 새 도구 추가 시 그 도구의 `Transport()` 선언만으로 확장.
- **PR-12d (모드 스위치)**: 스캔이 financial 모드면 `Fetcher`는 `Transport.Proxy=proxyURL`, probe 어댑터는 `HTTP_PROXY` env로 구동(testssl 제외). 비-financial이면 직접(현행). scan 유닛 변경은 경미하며 arch-check 불변(proxy 주소는 설정값, scan은 여전히 domain만 import).
- **PR-12e (탭풀 T, Q4=A)**: proxy는 단일 컨텍스트 위 동시 요청 슬롯 T개(기본 6, `--browser-tabs`). 순수 HTTP는 APIRequestContext(동시 안전), JS 렌더분은 Page(탭).

## 관측성 (PR-9, US-028, Q5=A)

- `ProgressReporter`가 WorkItem enqueue/claim/done/failed 이벤트로 카운터 갱신. 상태 API가 `ProgressSnapshot` 노출. OTel/메트릭 exporter는 인터페이스 뒤 Infra 이연.

## Infra-교체 가능 seam (PR-11, 사용자 강조)

- **모든 저장/큐/관측 seam은 Infra 단계에서 인터페이스를 바꾸지 않고** Redis/Valkey·Mongo·OTel 구현으로 교체된다(E-6). MVP in-memory 구현은 seam의 한 구현일 뿐.
- 시그니처 규칙(E-6): 첫 인자 `context.Context`, 마지막 반환 `error`, CAS는 `(bool, error)`, 영속 엔티티는 직렬화 가능(값 복사 반환).
- **PR-11a (계약 테스트)**: 각 seam에 대해 **구현-독립 계약 테스트**를 작성(인터페이스에 대해 실행) — MVP in-memory가 통과하면 Infra 구현도 같은 테스트로 검증 가능. `Orchestrator`/워커 루프는 구체 구현이 아니라 인터페이스에만 의존(주입) → Infra 교체 시 오케스트레이션 코드 무변경.
- **PR-11b**: `cmd/chiller`(composition root)만이 어떤 구현을 주입할지 안다. `--store=mem|mongo`, `--queue=mem|redis` 같은 플래그로 선택(MVP는 mem만 등록, Infra에서 나머지 추가). 오케스트레이션/도메인 코드는 이 선택을 모른다.

## 불변식 요약 (PR-10)
1. 모든 스캔의 커버리지는 `Validate(spec)` 통과 (26분할). 
2. 종료 상태에서 나가는 전이 없음; 모든 전이는 CAS.
3. 미승인 호스트로 아웃바운드 0 (data plane gate가 보장, platform은 재확인 안 함 — 단일 책임).
4. URL당 최대 1 GET(재개 포함).
5. seam 인터페이스는 Infra 백엔드 교체 시 불변 (PR-11).
