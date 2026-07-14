# Functional Design Plan — Unit `platform` (U1, last unit)

**Stage**: 🟢 CONSTRUCTION → `platform` → Functional Design (Part 1: Planning) · **Date**: 2026-07-08
**Inputs**: `inception/application-design/{services,components,component-methods,component-dependency}.md` · scan/findings 공개 API (완성됨) · `domain` 커널.

**이 유닛의 정체**: control plane + **composition root**. 이미 만들어진 data plane(scan/findings)을 end-to-end로 배선합니다. 새 판정 로직 없음 — 대신 **수명주기(FSM)·분배(fan-out/queue)·재개(CAS)·엣지(API/authz)·조립(pipeline)**.

---

## A. 이미 확정된 골격 (Application Design, 재논의 대상 아님)

| 요소 | 확정 내용 |
|---|---|
| 컴포넌트 | `ScanAPI` · `ScanOrchestrator` · `TaskStore` · `WorkQueue` · `ProgressReporter` · `FindingsPipeline` |
| FSM | `created → running → {completed \| stopped \| failed}`, `running→running` 재개 재진입 (US-024) |
| Fan-out | 체커를 `Scope()`로 분류 → WorkItem 생성 (URL-scoped: URL×전체 URL체커 / host-scoped: host×체커1) (C-6) |
| Resume | `TaskStore` WorkItem 완료 원장 = 진실원천, `CompareAndSetState`로 정확히 1 워커 소유 (US-025) |
| Pipeline | Normalize→Dedup→Severity→Compliance→Account→Report (findings, 완성됨) |
| 토폴로지 | 단일 이미지, `--role=manager\|worker`, `cmd/chiller`가 배선 (A-1) |
| Import 규칙 | `platform`은 `domain` + data-plane **인터페이스**만 import; 아무도 platform을 import 안 함 (NFR-02, arch-check) |

**배선 대상 (이미 존재하는 API)**:
- scan: `exec.Executor.Run(ctx, wi, cache) domain.WorkItemOutcome` · `registry.OwnershipForFindings() → CWEOwnership` · `ingest.Ingest → URLSet{URLs, Options}` · `safety.{ApprovalGate, RateLimiter}` · `fetch.{Fetcher, ResponseCache, Discoverer}`
- findings: `normalize/dedup/severity/compliance/coverage/report/store` + `coverage.New(ownership, mt, spec)` / `Accountant.Account(ScanOutcome)` / `report.Build(scanID, findings, cov, frameworks)`
- domain: `WorkItem` / `WorkItemOutcome{Findings, Failed, NotApplicable}` / `Coverage` / `CoverageSpec`

---

## B. 이 단계가 확정할 것 (Functional Design 산출물)

1. **Scan(Task) 애그리거트 + WorkItem 완료 원장**의 정확한 엔티티/상태 필드 (`domain-entities.md`)
2. **FSM 전이표** + 각 전이의 사전/사후조건 + CAS 규칙 (`business-rules.md`)
3. **오케스트레이션 시퀀스**: CreateScan→StartScan→fan-out→enqueue→worker loop→outcome 집계→pipeline→report; ResumeScan; StopScan (`business-logic-model.md`)
4. **WorkItemOutcome → coverage.ScanOutcome 변환 규칙** (Failed→ToolFailed, NotApplicable map→PS-ID N/A) — findings 회계 입력
5. **부팅 완전성 게이트**: 기동 시 `coverage.New`가 성공(26 커버) 해야 스캔 수락 (R5, cross-unit wiring test가 이미 증명한 불변식을 런타임 게이트로)

**아래 §C의 5개 MVP-범위 질문 답변 후** 산출물을 생성합니다.

---

## C. 설계 질문 (MVP 범위 — 모든 `[Answer]:` 를 채워주세요)

> scan에서 Valkey/Mongo/CLI 번들을 Infra로 이연한 것과 **정합**을 맞추는 게 핵심입니다.

### Q1 — TaskStore / WorkQueue 의 MVP 백엔드
FSM 상태·WorkItem 완료 원장·큐를 MVP에서 무엇으로 뒷받침할까요?

A) **인터페이스 + in-memory 구현 (AI 권장)** — `TaskStore`/`WorkQueue`를 인터페이스로 정의하고 MVP는 in-memory 구현. Redis/Valkey·Mongo 구현은 **Infra 단계**로 이연(scan의 ResponseCache=InMemory, findings의 store=InMemory와 동일 원칙). CAS-resume **로직**은 완전히 구현·테스트하되, MVP 백엔드가 in-memory라 "프로세스 재시작 후 재개"는 durable하지 않음(Warm-Standby는 Infra에서 Redis 구현 붙이면 활성). 프로세스 내 재개(워커 크래시 후 재분배)는 동작.
**(권장 사유: 유닛 전체가 순수 Go로 빌드/테스트되고, cloud 백엔드 의존이 Infra로 격리됨. 앞선 3유닛과 일관)**

B) **지금 Redis/Valkey + Mongo 실제 구현** — durable resume가 MVP부터 동작하나, 외부 런타임 의존·테스트 컨테이너 필요, Infra 결정 선점

C) in-memory만, 인터페이스 없이 — 가장 단순하나 NFR-04(교체성)·NFR-06(Warm-Standby) seam 상실

[Answer]: A

### Q2 — 프로세스/역할 모델 (MVP 실행 형태)
`--role=manager|worker`는 확정됐지만, MVP에서 **실제로 어떻게 기동**할까요?

A) **`--role=all` 단일 프로세스 기본 + manager/worker 분리도 가능 (AI 권장)** — MVP 기본은 한 프로세스가 manager+worker 둘 다(WorkQueue=in-process 채널/큐). `--role=manager`/`worker`로 분리 기동도 지원하되, 분리 모드의 큐는 Q1 백엔드에 의존(in-memory면 분리 불가 → Infra에서 Redis 붙으면 활성). 로컬/standalone 실행(NFR-04) 즉시 가능.
**(권장 사유: 단일 프로세스로 end-to-end 데모·테스트가 되고, 수평확장 seam은 보존)**

B) 항상 분리(manager 별도 / worker 별도) — coolant와 동일하나 MVP에 외부 큐 강제(Q1=B 필요)

C) 단일 프로세스만, role 플래그 없음 — 가장 단순하나 확장 seam 상실

[Answer]: A

### Q3 — ScanAPI 전송 계층 범위 (US-001/023)
제출/상태/리포트 API를 MVP에서 어디까지?

A) **`net/http` REST + 얇은 핸들러, 오케스트레이터는 Go 인터페이스 (AI 권장)** — `ScanOrchestrator`를 순수 Go 인터페이스로 두고(프로그램적 호출·테스트 용이), 그 위에 stdlib `net/http` 핸들러 3개(`POST /scans`, `GET /scans/{id}`, `GET /scans/{id}/report`). 외부 프레임워크 없음. authz는 Q4.
**(권장 사유: 외부 비통제 소비자(P4) 존재하므로 HTTP 표면이 필요, 그러나 라우터 프레임워크는 과함. coolant httpapi 패턴 AWS-strip)**

B) 오케스트레이터 Go API만, HTTP는 Post-MVP — 지금은 인-프로세스 호출/CLI만. 외부 소비자 계약 지연

C) 풀 REST + OpenAPI 스펙 + 라우터 — 계약 견고하나 MVP 과투자

[Answer]: A

### Q4 — ScanAPI authz 깊이 (US-023, SEC)
API 인가를 MVP에서 어떻게?

A) **`Authorizer` 인터페이스 + Bearer-token 구현 (AI 권장)** — 요청마다 `Authorizer.Authorize(req) error` 훅, MVP 기본 구현은 설정된 정적 토큰(Bearer) 검사. RBAC/OIDC는 인터페이스 뒤로 이연(Infra/NFR). fail-closed(토큰 없거나 불일치 → 401).
**(권장 사유: safety.ApprovalGate와 동일 fail-closed 철학, seam 보존, MVP 과투자 회피)**

B) mTLS / OIDC 지금 구현 — 강하나 Infra 선점·복잡

C) authz 없음(내부 신뢰망 가정) — MVP 단순하나 SEC 위반, 외부 소비자엔 부적합

[Answer]: A

### Q5 — 관측성(ProgressReporter) + 부팅 게이트 MVP 형태
US-028(진행률/헬스) + R5 부팅 완전성을 MVP에서 어떻게?

A) **`ProgressReporter` 인터페이스 + 인-메모리 카운터 구현 + 기동 시 부팅 완전성 게이트 강제 (AI 권장)** — 진행률은 체커별/스캔별 카운터(스캔 상태 API로 노출). OTel exporter 배선은 Infra 이연(인터페이스만). 기동 시 `coverage.New(ownership, mapping, spec)` 실패하면 스캔 수락 거부(26 커버 못 하면 부팅 실패) — cross-unit wiring test가 검증한 불변식의 런타임 강제.
**(권장 사유: 관측 seam 보존 + OTel 런타임 의존은 Infra로, 그리고 "26 커버 보장"을 코드로 강제)**

B) OTel 지금 풀 배선 — 관측 완비하나 Infra 선점

C) 관측 최소(로그만) + 부팅 게이트만 — 가장 단순

[Answer]: A

---

## D. 실행 체크리스트
### Part 1 — Planning
- [x] D-1. 골격 재확인(§A) + FD 확정 대상(§B) 정리
- [x] D-2. MVP-범위 질문 5개 작성(§C)
- [x] D-3. 답변 수집 (Q1-5=A) → Step 9: 충돌·공백 없음, 전부 정합
- [x] D-4. 답변 확정

### Part 2 — Artifact Generation (승인 후)
- [x] D-5. `domain-entities.md` — E-1..E-5 (Scan/ScanState/WorkLedger/Progress/seam)
- [x] D-6. `business-rules.md` — PR-1..10 (FSM 전이표 T1-6, CAS, PR-5 변환, 부팅 게이트, authz)
- [x] D-7. `business-logic-model.md` — 부팅/제출/워커루프/finalize/조회/resume/stop 시퀀스 + 통합테스트 계획
- [ ] D-8. 완료 메시지(2-option) 제시 → 승인 (진행 중)

## 확정된 결정 (재확인 불필요)
Application Design §A 전부 · 앞선 3유닛 API 시그니처(스캔 리뷰 후 `Fetch(ctx,…)`/`Run(ctx,…)`/`exec.New(…,gate)`/`URLSet.Options` 포함).
