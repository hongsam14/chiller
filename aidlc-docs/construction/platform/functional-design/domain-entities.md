# Functional Design — Unit `platform` (U1) · Domain Entities

**Stage**: 🟢 CONSTRUCTION → `platform` → Functional Design · **Date**: 2026-07-08
**Decisions**: Q1-5 = A. platform = control plane + composition root; seam(인터페이스)은 지금, cloud 백엔드(Redis/Mongo/OTel/OIDC)는 Infra 이연.

Platform은 판정 도메인 타입을 **새로 만들지 않습니다** — `domain` 커널의 `WorkItem`/`WorkItemOutcome`/`Coverage`/`ScanID`/`CheckerID` 를 재사용합니다. 아래는 platform이 **소유**하는 오케스트레이션 엔티티(FSM·원장·진행률)로, `internal/platform` 안에만 존재하고 data plane은 이들을 모릅니다(NFR-02).

---

## E-1. `Scan` 애그리거트 (오케스트레이션 단위 = 사용자 1 분석 요청)

| 필드 | 타입 | 의미 |
|---|---|---|
| `ID` | `domain.ScanID` | 스캔 식별자 |
| `State` | `ScanState` | FSM 상태 (E-2) |
| `Request` | `ScanRequest` | 원본 요청 (URLs, Approved 호스트, `ingest.ScanOptions`) |
| `Frameworks` | `[]domain.Framework` | 리포트 투영 프레임워크 (MVP: KISA) |
| `CreatedAt` / `UpdatedAt` | `domain.Timestamp` | 수명주기 타임스탬프 (주입, 시간은 순수성 위해 외부) |
| `Report` | `*report.Report` | 완료 시 조립된 최종 리포트 (그 전엔 nil) |

- **Task = Scan** (AC0 2계층의 상위). fan-out 단위 WorkItem은 하위. Scan은 `TaskStore`에 영속.
- `Request`는 `ingest.Ingest`가 검증·기본값 적용한 `URLSet.Options`(H3 수정)를 담는다 — 리미터/바운드의 유효 설정 원천.

## E-2. `ScanState` (FSM 상태 — US-024)

```
created ──StartScan──▶ running ──(모든 WorkItem 종료)──▶ completed
   │                     │  │
   │                     │  └──StopScan──▶ stopped
   └──StopScan──▶ stopped│
                         └──(치명 오류)──▶ failed
 running ──ResumeScan──▶ running   (재진입; 미완 WorkItem 재분배)
```

| 상태 | 종류 | 의미 |
|---|---|---|
| `created` | 비종료 | Scan 수락·영속됨, fan-out 전 |
| `running` | 비종료 | WorkItem 분배·실행 중 (재개 시 재진입) |
| `completed` | **종료** | 모든 WorkItem 종료 + 커버리지 회계 + 리포트 조립 완료 |
| `stopped` | **종료** | 사용자 중단 (US-…): 진행 중 item은 자연 종료, 미분배 item은 취소 |
| `failed` | **종료** | 복구 불가 오류(예: 부팅 게이트는 통과했으나 저장소 치명 오류) |

## E-3. `WorkLedger` — WorkItem 완료 원장 (CAS-resume 진실원천, US-025)

`TaskStore`가 Scan별로 보유. `domain.WorkItem`을 확장하지 않고 상태만 추적:

| 필드 | 타입 | 의미 |
|---|---|---|
| `Item` | `domain.WorkItem` | 분배 단위 (fan-out 산출) |
| `State` | `ItemState` | `pending → inflight → {done \| failed}` |
| `Outcome` | `*domain.WorkItemOutcome` | `done` 시 결과 (findings/failed/NA) |

- **CAS 청구**: 워커가 `pending → inflight` 를 `CompareAndSetItemState`로 원자 전이 → 정확히 1 워커가 소유(US-025). 
- **재개**: `ListUnfinished(scanID)` = `pending ∪ inflight` (크래시한 inflight 포함) → 재큐. 완료(`done`/`failed`)는 재실행 안 함.
- Q1=A: MVP 원장은 in-memory. durable 재개(프로세스 재시작)는 Infra의 Redis/Mongo 구현에서 활성; 프로세스 내 워커 크래시 재분배는 MVP에서 동작.

## E-4. `ProgressSnapshot` (US-028)

| 필드 | 타입 | 의미 |
|---|---|---|
| `ScanID` | `domain.ScanID` | |
| `State` | `ScanState` | |
| `Total` / `Done` / `Failed` / `Inflight` | `int` | WorkItem 카운트 |
| `PerChecker` | `map[domain.CheckerID]struct{OK,Failed int}` | 체커별 집계 |

- Q5=A: 인-메모리 카운터. `ScanAPI` 상태 엔드포인트로 노출. OTel exporter는 `ProgressReporter` 인터페이스 뒤로 Infra 이연.

## E-5. 소유 인터페이스 (seam, code gen에서 구현)

> **핵심 제약 (사용자 강조, E-6)**: MVP는 in-memory로 구현하지만 **Infra 단계에서 인터페이스를 바꾸지 않고** Redis/Valkey·Mongo 구현으로 교체한다. 따라서 seam 시그니처는 **지금부터 네트워크 백엔드를 견디도록** 설계한다 — in-memory가 당장 안 써도 `context.Context`·`error`를 시그니처에 포함.

**Infra-ready seam 규칙 (E-6)** — 모든 저장/큐/관측 seam 메서드에 적용:
1. 첫 인자는 `ctx context.Context` (네트워크 취소/데드라인/추적).
2. 마지막 반환은 `error` (in-memory는 대개 nil이나 시그니처엔 존재).
3. CAS는 `(swapped bool, err error)` — 네트워크 저장소에서도 표현 가능.
4. 영속 엔티티는 **직렬화 가능** — 함수/채널/포인터-그래프 금지, 값 복사로 반환(스토어가 내부 상태를 노출하지 않음).
5. Dequeue는 **ctx로 블록**(채널이든 Redis BLPOP이든 동일 계약); at-least-once + 가시성은 CAS 청구(PR-4)로 흡수.

```go
type TaskStore interface {
    SaveScan(ctx context.Context, s Scan) error
    LoadScan(ctx context.Context, id domain.ScanID) (Scan, error)
    CompareAndSetState(ctx context.Context, id domain.ScanID, expected, next ScanState) (bool, error)
    AppendWorkItems(ctx context.Context, id domain.ScanID, items []domain.WorkItem) error
    CompareAndSetItemState(ctx context.Context, wid domain.WorkItemID, expected, next ItemState) (bool, error)
    RecordOutcome(ctx context.Context, wid domain.WorkItemID, o domain.WorkItemOutcome) error
    ListUnfinished(ctx context.Context, id domain.ScanID) ([]domain.WorkItem, error) // pending ∪ inflight
    AllOutcomes(ctx context.Context, id domain.ScanID) ([]domain.WorkItemOutcome, error)
    SaveReport(ctx context.Context, id domain.ScanID, r report.Report) error
    LoadReport(ctx context.Context, id domain.ScanID) (report.Report, bool, error)
}

type WorkQueue interface {
    Enqueue(ctx context.Context, item domain.WorkItem) error
    Dequeue(ctx context.Context) (domain.WorkItem, error) // blocks until item or ctx done
    Drain(ctx context.Context, id domain.ScanID) error    // cancel undispatched items of a scan
}

type ProgressReporter interface {
    Init(ctx context.Context, id domain.ScanID, total int) error
    OnClaimed(ctx context.Context, wid domain.WorkItemID) error
    OnFinished(ctx context.Context, id domain.ScanID, o domain.WorkItemOutcome) error
    Snapshot(ctx context.Context, id domain.ScanID) (ProgressSnapshot, error)
}

type Authorizer interface { // req는 자체 ctx 보유 → 별도 ctx 인자 불필요
    Authorize(r *http.Request) error // fail-closed; nil=허용
}

// --- 금융권 인증 transport (browser-proxy-transport-clarification.md, Q1=B/Q2=A/Q3=B/Q4=A) ---

// BrowserSession은 단 하나의 인증 브라우저 컨텍스트(storageState 주입)를 소유한다.
// Playwright 구현 + 테스트용 fake. 동시 요청은 탭풀 T로 제한(컨텍스트 쿠키 공유).
type BrowserSession interface {
    // Do는 인증 세션 쿠키/헤더를 주입해 요청을 대상으로 보내고 응답을 반환한다.
    // 순수 HTTP는 APIRequestContext, JS 렌더 필요 시 Page(탭). T 슬롯으로 blocking.
    Do(ctx context.Context, req *http.Request) (*http.Response, error)
    Close(ctx context.Context) error
}

// AuthProxy는 worker의 아웃바운드를 받아 BrowserSession으로 재요청(세션 주입)하는
// 로컬 포워드 프록시. HTTPS는 번들 CA로 MITM(worker가 신뢰). proxy 역할이 구동.
type AuthProxy interface {
    Serve(ctx context.Context, addr string) error // 블록; ctx 취소 시 종료
}

type Orchestrator interface {
    CreateScan(ctx context.Context, req ScanRequest) (domain.ScanID, error)
    StartScan(ctx context.Context, id domain.ScanID) error
    ResumeScan(ctx context.Context, id domain.ScanID) error
    StopScan(ctx context.Context, id domain.ScanID) error
    GetStatus(ctx context.Context, id domain.ScanID) (ProgressSnapshot, error)
    GetReport(ctx context.Context, id domain.ScanID) (report.Report, bool, error)
}
```

| 인터페이스 | MVP 구현 | Infra 교체 대상 (인터페이스 불변) |
|---|---|---|
| `TaskStore` | in-memory(맵+뮤텍스) | Mongo(Scan/원장 컬렉션) |
| `WorkQueue` | in-process(버퍼 채널) | Redis/Valkey(asynq류 BLPOP+visibility) |
| `ProgressReporter` | 인-메모리 카운터 | + OTel exporter |
| `Authorizer` | Bearer 정적 토큰 | OIDC/mTLS |
| `Orchestrator` | 순수 Go | 불변(백엔드 비의존) |
| `BrowserSession` | Playwright(Chromium) + storageState | 세션 자동 갱신, JS 토큰 회전 |
| `AuthProxy` | 로컬 MITM 포워드 프록시 | HTTPS 인터셉션 하드닝 |

**역할 4종 (Q3=B)**: `all`(개발/standalone) · `manager`(API+FSM+조립) · `worker`(체커 실행, 앱계층 아웃바운드는 proxy 경유) · **`proxy`**(단일 인증 세션 보유, worker 트래픽에 세션 주입 후 포워딩). worker 다수가 **한 proxy**를 가리켜 단일 세션 불변식 유지하며 수평 확장.

**패키지 배치**: `internal/platform/{orchestrator, taskstore, workqueue, api, progress, proxy}` + `cmd/chiller`(composition root, `--role`). 각 seam은 인터페이스 + `*_mem.go`(MVP 구현)로 분리해 Infra 구현(`*_redis.go`/`*_mongo.go`)이 같은 패키지에 나란히 추가되게 한다. `proxy`의 `BrowserSession`은 인터페이스+Playwright 구현+fake로 분리(테스트 시 브라우저 불요).
