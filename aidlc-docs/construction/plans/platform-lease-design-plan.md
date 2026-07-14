# Design Plan — platform (U1) Ledger Lease (recovery-model increment)

**Stage**: CONSTRUCTION → Design (platform U1 amendment — recovery model)
**Scope**: Add a **lease** to the WorkItem ledger so a control-plane recovery sweep can safely reclaim only *genuinely-stale* inflight items — closing the split-topology mid-run recovery gap flagged in the infra-increment code review (finding [1]), without the double-execution hazard that made me restrict recovery to boot-only/all-role.
**Also in scope**: re-enable a safe **periodic** recovery sweep (manager + all), and add the **dashboard scan list** (now feasible via lease-aware state).

**Design inputs (loaded)**:
- `internal/platform/types.go` (`WorkLedgerEntry{ID,ScanID,Item,State,Outcome}`), `seams.go` (`TaskStore`, `CompareAndSetItemState(wid,expected,next)`, `ListScansByState`), `orchestrator/{resume,worker}.go` (ResumeScan resets inflight→pending + re-enqueues; workerLoop claims via CAS).
- `cmd/chiller/main.go` `recoverOrphans` (boot-only, all-role — the thing this increment generalizes).
- `construction/platform/code/infra-business-logic-summary.md`, `operations/production-readiness.md` §limitation 1.
- Extensions (all Enabled/blocking): security-baseline, resiliency-baseline, property-based-testing (FULL).

---

## 0. Problem statement (why a lease)

Today the pending→inflight CAS claim marks an item inflight but records **nothing about who holds it or until when**. So a recovery sweep cannot tell "a live worker is processing this" from "the worker died". The infra increment therefore:
- ran recovery **only at boot, only in `all` role** (safe because a restarted single process has no concurrent live workers), and
- **could not** recover a worker that dies while the manager lives (split topology), nor run a periodic sweep (it would reset live items → double-execute → violate FR-02).

A **lease** (owner + expiry, refreshed by a heartbeat) makes "stale" decidable: the sweep reclaims an inflight item **only if its lease has expired**, which is safe because an expired lease means no live worker is still on it. This is the same visibility-timeout idea the Valkey `XAUTOCLAIM` reaper uses, lifted to the ledger (the recovery source of truth).

---

## 1. Execution plan (checkboxes)

### Phase 0 — Design decisions (this doc)
- [x] P0-1: Load current recovery code + review finding [1] + production-readiness limitation 1
- [x] P0-2: State the problem (§0) and the lease principle
- [x] P0-3: Questions answered + contradiction check (§2)

### Phase 1 — Design artifacts (after answers)
- [x] P1-1: `construction/platform/functional-design/lease-recovery.md` — entities (lease fields), business rules (claim stamps lease, heartbeat refreshes, sweep reclaims expired-only), the safe-reclaim invariant, PBT properties
- [x] P1-2: Seam evolution note — exact signature changes (both mem + Mongo implement; PR-11 impact assessed)
- [x] P1-3: Extension compliance table (SECURITY/RESILIENCY/PBT)
- [x] P1-4: Completion message + approval gate

### Phase 2 — Code Gen (separate stage, after design approval)
- [ ] P2-1: ledger lease fields + claim-with-lease + heartbeat + expired-inflight query (mem + Mongo, contract suite extended)
- [ ] P2-2: safe reclaim sweep (periodic, manager+all) replacing boot-only `recoverOrphans`
- [ ] P2-3: dashboard scan list endpoint + UI
- [ ] P2-4: PBT (no double-claim under concurrent claim+expire), tests, verify green

---

## 2. Design questions

**답변 방법**: 각 `[Answer]:` 뒤에 문자를 적어 주세요. 없으면 `X) Other`. 다 쓰시면 "완료"라고 알려 주세요.

---

## Question 1
**lease를 원장에 어떻게 표현**합니까? (WorkLedgerEntry에 필드 추가)

A) **owner + expiry 두 필드** — `LeaseOwner string`(워커/컨슈머 id) + `LeaseExpiry domain.Timestamp`. claim 시 둘 다 스탬프, sweep은 `state==inflight && LeaseExpiry < now`면 회수. 사람이 읽기 쉽고 "누가 붙잡고 있었나"까지 보임(디버깅·대시보드).

B) **expiry 한 필드만** — `LeaseExpiry domain.Timestamp`만. owner 불필요(누가 죽었는지 알 필요 없음, 만료만 보면 됨). 더 단순, 필드 1개.

C) **claim 시각 + 고정 TTL** — `ClaimedAt domain.Timestamp`만 저장, TTL은 설정 상수. sweep은 `now - ClaimedAt > TTL`. heartbeat로 TTL 갱신하려면 ClaimedAt을 밀어야 함(=expiry와 동형). 사실상 B와 유사하나 만료 계산이 읽는 쪽에 분산됨.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

---

## Question 2
**heartbeat(리스 갱신)** 를 둡니까? 일부 체커(testssl, dirhunt)는 느려서 단일 TTL을 넘길 수 있습니다.

A) **heartbeat 있음** — 워커가 item 처리 중 주기적으로 lease를 연장(`RefreshLease(wid, newExpiry)`). 느린 체커도 살아있으면 회수 안 됨. 정확하지만 워커 루프에 갱신 호출 추가(오케스트레이터 소폭 변경) + seam에 refresh 메서드.

B) **heartbeat 없음, 넉넉한 TTL** — TTL을 가장 느린 체커의 최악 실행시간보다 크게(예: per-request 타임아웃 × 체커 수 + 여유). 갱신 로직 없음(가장 단순). 단점: 죽은 워커의 item이 TTL 만큼 회수 지연됨(=긴 TTL이면 복구 느림).

C) **heartbeat 없음, per-scan ctx 타임아웃과 연동** — 이미 per-request 타임아웃(SR-8)과 scan ctx가 있으므로, item 실행 상한이 곧 실질 TTL. lease TTL = 그 상한 + 여유로 자동 산정.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

---

## Question 3
**claim과 lease 스탬프를 원자적으로** 하려면 `CompareAndSetItemState(wid, expected, next)` 로는 lease를 못 실습니다. seam을 어떻게 진화합니까? (PR-11 "인터페이스 불변"은 infra swap 때 원칙이고, 이건 기능 변경이라 진화는 허용되나 최소화하고 싶음)

A) **claim 전용 메서드 추가** — `ClaimItem(ctx, wid, lease Lease) (bool, error)`(pending→inflight + lease 스탬프를 findOneAndUpdate 한 방에). 기존 `CompareAndSetItemState`는 sweep의 reset(inflight→pending)·done 전이용으로 유지. 의미가 명확히 분리됨.

B) **기존 CAS에 lease 인자 추가** — `CompareAndSetItemState(wid, expected, next, lease *Lease)`. 시그니처 1개만 바꾸고 모든 전이가 통과. 호출부 전부 수정(대부분 nil lease).

C) **lease를 별도 메서드로 분리** — claim은 기존 CAS 그대로, 직후 `SetLease(wid, lease)` 별도 호출. 원자성 없음(claim 성공 후 lease 스탬프 사이 크래시 시 lease 없는 inflight 발생 → sweep이 즉시 회수 가능한 창). 가장 적게 바꾸지만 원자성 손실.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

---

## Question 4
sweep이 **만료된 inflight item을 어떻게 조회**합니까?

A) **전용 쿼리 메서드** — `ListExpiredInflight(ctx, scanID, now) ([]WorkItem, error)`. Mongo는 `{scanId, state:inflight, leaseExpiry:{$lt:now}}`로 인덱스 활용. 정확하고 효율적. seam에 메서드 1개 추가.

B) **기존 ListUnfinished 재사용 + 클라이언트 필터** — 전체 unfinished를 받아 sweep이 메모리에서 만료 여부 판단. 메서드 추가 없음. 단, WorkItem에 lease가 안 실려 오면(현재 안 실림) 원장 엔트리 노출이 필요 → 결국 반환형/메서드 변경.

C) **sweep이 reclaim CAS로 위임** — 조회 없이, `ReclaimExpired(ctx, scanID, now) ([]WorkItem, error)`가 만료 inflight를 pending으로 CAS-리셋하고 그 목록을 반환. 조회+리셋을 한 메서드로(원자적 재활성화). sweep은 반환된 것만 재큐잉.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

---

## Question 5
**복구 sweep의 위치와 주기**는? (지금은 boot-only/all-role인 `recoverOrphans`)

A) **manager + all에서 주기적 sweep(권장)** — lease 덕에 안전해졌으므로, manager(컨트롤 플레인)와 all이 주기적으로(예: 30s) 만료 inflight를 회수·재큐잉. boot sweep은 유지(즉시 복구). 분리 토폴로지의 worker 사망도 다음 주기에 복구. worker 역할은 sweep 안 함(컨트롤 플레인 책임).

B) **boot + 주기, all만** — 분리 토폴로지는 여전히 제외. 가장 보수적이지만 이번 증분의 목적(분리 복구)을 절반만 달성.

C) **주기만, boot 생략** — 첫 주기까지 복구 지연 허용. 단순하나 재기동 직후 공백.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

---

## Question 6
**대시보드 스캔 목록** 스코프는? (지금 UI는 큐 통계 + 스캔 id 조회만)

A) **running + 최근 종료 목록** — `/api/introspect/scans`가 `ListScansByState(running)` + completed/stopped/failed 일부를 반환, 각 스캔 ProgressSnapshot(Total/Done/Failed/Inflight). UI에 표로. 운영 가시성 최대.

B) **running만** — 진행 중 스캔만 나열. 종료된 건 id 조회로. 더 가벼움.

C) **이번 증분에선 UI 스캔 목록 제외** — lease/복구에만 집중, 대시보드 목록은 다음으로 미룸.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

---

## Question 7
**lease TTL 기본값**(수치)은? (Q2 선택과 연동)

A) **60초** — Valkey reaper MinIdle과 동일. 빠른 복구, heartbeat 있으면(Q2=A) 느린 체커도 안전.

B) **300초(5분)** — 느린 체커(testssl/dirhunt) 여유. heartbeat 없이도(Q2=B) 대부분 커버. 복구 지연은 최대 5분.

C) **per-request 타임아웃 기반 자동 산정** — 코드가 SR-8 타임아웃 × 안전계수로 계산(Q2=C와 정합). 고정 상수 없음.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

---

## 3. 다음 단계
1. Q1–Q7 답변 → "완료"
2. 모순/모호성 검사 → 필요 시 clarification
3. Phase 1 설계 아티팩트 생성 → 승인 게이트
4. 승인 후 Code Gen(Phase 2)
