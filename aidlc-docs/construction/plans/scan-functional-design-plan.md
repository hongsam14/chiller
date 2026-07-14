# Functional Design Plan — Unit `scan` (U2)

**Stage**: 🟢 CONSTRUCTION → Unit `scan` → Functional Design (Planning) · **Date**: 2026-07-08
**Unit**: U2 data plane (`internal/scan`) — Ingestion · Safety · Fetch+Cache · Checking (Observation native engine 10 + Probe CLI 7). Emits `domain.Finding`s + `WorkItemOutcome`.
**Inputs**: `../../inception/application-design/{components,component-methods}.md` · requirements r5 · `../domain/functional-design/*` · `../findings/functional-design/*` (it consumes findings' RawFinding/ownership) · stories US-002~013, 030~033.
**Outputs (Part 2)**: `../scan/functional-design/{business-logic-model, business-rules, domain-entities}.md`

**Scope**: contracts + algorithms for scan components. Consumes domain's `Checker`/`ScanContext`/`HAREntry`/`Target`/`WorkItem` (fixed). One deliberate **domain addition** is proposed (FQ5, see 질문 2).

---

## A. Components (from Application Design)
`URLIngestor` (US-003) · `ApprovalGate` (US-004) · `RateLimiter` (US-005) · `Fetcher`+`ResponseCache` (US-002, C-8 JS discovery) · `CheckerRegistry` + `ObservationEngine` (10 signatures, US-006/008-013/030-033) · `ProbeAdapter`×7 (US-007).

## B. Execution checklist
### Part 1 — Planning
- [x] B-1. Component analysis (§A)
- [x] B-2. Questions (§C)
- [x] B-3. Collect answers — 질문1-5 전부 A
- [x] B-4. Step 5 — 모호성 없음. 질문2=A → domain에 CheckResult+WorkItemOutcome.NotApplicable 적용(재검증 green)
- [x] B-5. (질문 완결; 산출물 생성 후 승인)
### Part 2 — Generation
- [x] B-6/7/8. domain-entities · business-rules · business-logic-model 생성
- [x] B-9. state/audit 갱신

---

## C. 기능 설계 질문 (모든 `[Answer]:` 를 채워 주세요)

### 질문 1 — Observation 시그니처 구조 & CWE 소유 등록 *(핵심)*
관찰 엔진은 10개 시그니처를 돌립니다. FQ1(체커가 CWE 소유)에 따라 시그니처는 CWE로 키잉됩니다. 구조를 어떻게?

A) **시그니처 1개 = CWE 1개 (AI 권장)** — 각 `ObservationSignature`가 자신이 방출하는 **단일 CWE**를 선언(`CWE() domain.CWE`)하고 `Judge(*HAREntry) []Finding` 구현. 엔진이 시그니처 목록에서 `CWEOwnership`을 자동 도출(`observation:CWE-615 → [CWE-615]`). PS-009처럼 CWE 여러 개인 항목은 **여러 시그니처**(CWE-615 주석, CWE-540 소스, CWE-200 정보노출)로 자연 분해.

B) 시그니처 1개 = PS-항목 1개, 여러 CWE 방출 — PS 중심(구식 PS 키잉과 충돌, FQ1 위반)

X) 기타

[Answer]: A

### 질문 2 — Checker의 NotApplicable 신호 방법 *(domain 계약 변경, 필수)*
FQ5(구조적 N/A)를 위해 체커가 "이 항목은 대상에 적용 불가"를 신호해야 하는데, 현재 `Check(ScanContext) ([]Finding, error)`엔 채널이 없습니다.

A) **`CheckResult` 반환 타입 도입 (AI 권장)** — `Check(ScanContext) (CheckResult, error)`, `CheckResult{ Findings []Finding; NotApplicable []CWE }`. 그리고 `domain.WorkItemOutcome`에 `NotApplicable []CheckerID`(또는 per-CWE) 추가. N/A는 에러도 finding도 아닌 1급 결과. findings 회계가 이미 N/A 입력을 받도록 설계됨.

B) 별도 옵셔널 인터페이스 `NAReporter` — Checker는 그대로, N/A 낼 체커만 구현. 유연하나 오케스트레이터가 타입 체크

C) N/A를 지금은 미구현 (MVP는 실행=Checked) — domain 무변경, N/A는 Post-MVP

X) 기타

[Answer]: A

### 질문 3 — Probe 어댑터 실행 모델
7개 외부 CLI(humble/httpx/testssl/dirhunt/changeme/Corsy/subzy)를 어떻게?

A) **in-process subprocess exec + 어댑터당 파서 (AI 권장)** — 이미지에 번들된 버전핀 바이너리를 `exec.CommandContext`(타임아웃 NFR-12)로 실행, stdout/JSON을 어댑터가 `[]RawFinding`(소유 CWE 포함)으로 파싱. HTML-consuming(humble/httpx)은 ResponseCache 재사용, 나머지는 자체 I/O(EC3). 실제 도구별 파서는 어댑터별 상세(일부는 이번 코드생성에서 스텁).

B) 다른 모델 ([Answer])

X) 기타

[Answer]: A

### 질문 4 — 사전 승인 목록(ApprovalGate) 출처
A) **스캔 요청에 포함 + 선택적 전역 allowlist 교집합 (AI 권장)** — 요청이 승인 대상을 싣고, 배포 전역 allowlist가 있으면 교집합만 허용(fail-closed). 둘 다 config/요청에서.

B) 전역 config만 · C) 요청만

X) 기타

[Answer]: A

### 질문 5 — RateLimiter 알고리즘 & 스코프
A) **대상 host별 token bucket, 파라미터화(rate/burst) (AI 권장)** — NFR-13. host 단위 독립. 초과분 지연(대기) + 초과 메트릭.

B) 다른 방식 ([Answer])

X) 기타

[Answer]: A

---
## D. 확장 규칙 (Planning)
| Ext | 상태 |
|---|---|
| security-baseline | ApprovalGate/RateLimiter = Safety 모듈 격리(SEC-11); 버전핀(SEC-10); fail-closed(SEC-15). Blocking 없음 |
| resiliency-baseline | 시그니처/어댑터 독립 실패→tool-failed(US-027); 타임아웃(NFR-12). Blocking 없음 |
| property-based-testing | 시그니처 앵커 property(픽스처 양성/음성), 파서 total. 상세는 Code Gen. Blocking 없음 |
