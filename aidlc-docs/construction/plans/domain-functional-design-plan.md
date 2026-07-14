# Functional Design Plan — Unit `domain` (U0)

**Stage**: 🟢 CONSTRUCTION → Unit `domain` → Functional Design (Part: Planning) · **Date**: 2026-07-08
**Unit**: U0 shared kernel (`internal/domain`) — the inter-unit contract. No stories directly; every other unit implements against it.
**Inputs**: `../../inception/application-design/{components,component-methods,unit-of-work}.md` · requirements r4 §6/§7 · stories US-014/015/016/020/021 (semantics live here)
**Outputs (Part 2)**: `../domain/functional-design/{domain-entities.md, business-rules.md, business-logic-model.md}`

**Scope note**: domain is pure contract — technology-agnostic types + their invariants. No I/O, no infra. This stage fixes the *exact* shape and semantics of `Finding`, `ScanContext`, `Checker`, `Coverage`, `HAREntry`, and the CWE/PS-ID two-axis rules, plus the seeds of the §7 PBT properties.

---

## A. What this unit must pin down (from prior stages)

- `Finding` (CWE-keyed), `Severity`, `CheckerID`/`ScopeKind`, `PSID`, `Framework`, `Coverage`/`ItemStatus`/`DelegateTarget`, `ScanContext`, `HAREntry`, `Checker` iface, `NoCanonicalCWE` sentinel.
- Invariants that other units rely on: dedup identity (US-015), severity totality (US-016), coverage totality+disjointness (US-020, §7), delegation target (US-021), CWE↔framework projection contract (FR-14).

## B. Execution checklist
### Part 1 — Planning
- [x] B-1. Unit context analyzed (§A)
- [x] B-2. Questions (§C)
- [x] B-3. Collect answers — Q1=B, Q2=A, Q3a=A, Q3b=B(Info), Q4=A, Q5=A
- [x] B-4. Step 5 분석 완료 (DQ1=A, DQ2=A) — 모호성 전부 해소
- [x] B-5. (질문 완결; 산출물 생성 후 승인)

### Part 2 — Generation (after approval)
- [x] B-6. `domain-entities.md` — every type, fields, value ranges, relationships
- [x] B-7. `business-rules.md` — dedup identity, severity mapping, coverage invariants, projection contract, error/partial semantics
- [x] B-8. `business-logic-model.md` — how the types compose across a scan (contract-level); PBT property seeds (§7) restated at type level
- [x] B-9. Update aidlc-state.md

---

## C. Functional design questions (fill every `[Answer]:`)

### Question 1 — Finding **identity** (the dedup key, US-015 / FR-06)
"같은 finding"을 무엇으로 판정할까요? dedup 멱등·교환성(§7 property)이 이 정의에 달려 있습니다.

A) **(정규화된 CWE 집합 + target_url) — 도구 무관 dedup (AI 권장)** — 같은 URL에서 같은 약점이면, 어느 도구가 찾았든 1건. FR-06 "체커 간 이중 판정 없음"에 직결. 병합 시 evidence는 규칙으로 합성(§Q1b).

B) (CWE 집합 + target_url + source_tool) — 도구 단위 dedup만 (도구 간 중복은 남김)

C) (CWE 집합 + target_url + evidence 해시) — 가장 세밀, 사실상 dedup 거의 안 됨

X) 기타

[Answer]: B

**Q1b (A 선택 시) 병합 규칙**: 같은 (CWE, URL)이 여러 도구에서 나오면?
A) severity는 **최고값 채택**, evidence는 bounded 범위 내 **출처별 병합**, source_tool은 **집합으로 보존 (AI 권장)**
B) 먼저 도착한 것 유지, 나머지 폐기
X) 기타

[Answer]: X

### Question 2 — CWE 표현
A) **정규 문자열 `"CWE-540"` (AI 권장)** — MITRE 표기·매핑 테이블 키·리포트 가독성 일치. `no-canonical-cwe`는 예약 sentinel 문자열.

B) 숫자 `540` — 컴팩트하나 표기 변환 필요

X) 기타

[Answer]: A

### Question 3 — Severity 척도 + 매핑 총함수 (US-016)
**(a) 척도**
A) **`Info < Low < Medium < High < Critical` 순서형 5단계 (AI 권장)**
B) 다른 척도 ([Answer]에 서술)

[Answer]: A

**(b) 미지(unknown) 도구 severity 처리** — 총함수라 모든 입력에 출력 필요. 매핑 불가 값은?
A) **보수적으로 `Medium`으로 (AI 권장: 보안 스캐너는 과소보고 위험이 더 큼)**
B) `Info`로 (과다 알림 억제 우선)
C) 원본 문자열 보존한 `Unknown` 별도 등급 추가 (5단계 깨짐)
X) 기타

[Answer]: B. 기본적으로 recon에 해당되기 때문

### Question 4 — Checker 반환 계약 & tool-failed 의미 (US-027)
`Check(ScanContext) ([]Finding, error)` 에서 부분 실패를 어떻게?

A) **Check 단위는 all-or-nothing; 다중 항목 체커(ObservationEngine)는 시그니처 단위로 독립 성공/실패 (AI 권장)** — 한 Check가 error면 그 Check가 담당한 항목은 `tool-failed`, 성공하면 findings. ObservationEngine은 10개 시그니처를 각각 독립 실행해 일부만 실패해도 나머지는 정상 판정. 회계가 깔끔.

B) Check가 findings와 error를 **동시 반환**(부분 결과) — 유연하나 "부분 성공"의 회계가 복잡

X) 기타

[Answer]: A

### Question 5 — `ScanContext` / `HAREntry` 형상 확인
A) **아래 subset (AI 권장)**:
- `ScanContext{ Target, Response *HAREntry, DiscoveredJS []*HAREntry }` — URL-scoped 체커는 Response(+r4 발견 JS)를 읽고, host-scoped는 Target만.
- `HAREntry{ URL, Scheme, Status, Headers[], Cookies[], Body(bounded), FetchedAt }` — 전체 HAR이 아니라 관찰에 필요한 subset.

B) 전체 HAR 스키마 그대로 채택 (표준성↑, 무게↑)

C) 다른 형상 ([Answer]에 서술)

[Answer]: A

---

## D. 확장 규칙 준수 (Planning)
| Extension | 상태 |
|---|---|
| security-baseline | `HAREntry.Body`/`Finding.Evidence`는 bounded (FR-12); 민감값 redaction 규칙은 business-rules에 명시. Blocking 없음 |
| property-based-testing | §7 property 4종(정규화 oracle·dedup 멱등·severity 총함수·coverage 불변식)의 **타입 레벨 계약**을 business-logic-model에 기술 → 이후 유닛에서 구현. Q1/Q3/Q4 답이 이 property의 전제를 확정. Blocking 없음 |
| resiliency-baseline | tool-failed 상태(Q4)가 coverage 4상태의 계약. Blocking 없음 |

---

## E. Step 8 후속 (Q1=B / Q1b=X 잔여 해소)

**Step 5 판정**: Q2=A·Q3a=A·**Q3b=B(미지 severity→Info, 근거: recon 레벨 — 타당)**·Q4=A·Q5=A 확정. Q1=B는 아래 2건 확인 필요.

### DQ1. Q1=B와 FR-06 정합 확인
r3/r4에서 **각 PS 항목은 정확히 한 도구가 소유**(humble/observation/testssl/dirhunt/changeme/Corsy/subzy 겹침 없음). 따라서 도구 간 동일-항목 중복은 구조적으로 거의 발생 안 함.

A) **맞음 — FR-06은 "소유 분리 + 도구 내 dedup"으로 충족. 도구 간 dedup 안 함(출처 보존). (AI 권장)**

B) 아니오 — 도구 간에도 같은 (CWE집합, URL)이면 1건으로 합쳐야 함 → 이 경우 Q1=A로 회귀 + Q1b 병합규칙 필요

X) 기타

[Answer]: A

### DQ2. 같은 도구가 같은 `(CWE집합, target_url, source_tool)`을 두 번 방출 시 collapse 규칙
A) **1건으로 collapse; evidence는 bounded 범위 내 병합; severity 최고값 (AI 권장)**

B) 1건으로 collapse; 먼저 도착한 것 유지, 나머지 폐기

X) 기타

[Answer]: A

---
## 확정 요약 (DQ 답변 후 Part 2로)
Q1=B(키=CWE집합+URL+source_tool) · Q2=A(문자열 CWE) · Q3=A/Info(미지→Info, recon) · Q4=A(all-or-nothing, 시그니처 독립) · Q5=A(HAR subset) · DQ1/DQ2=?
