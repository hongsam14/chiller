# Functional Design Plan — Unit `findings` (U3)

**Stage**: 🟢 CONSTRUCTION → Unit `findings` → Functional Design (Planning) · **Date**: 2026-07-08
**Unit**: U3 data plane (`internal/findings`) — normalize → dedup → severity → map → account → report → store. Hosts most §7 PBT properties (P1–P7).
**Inputs**: `../domain/functional-design/*` (types + R1–R10 + P1–P7) · `../../inception/application-design/{components,component-methods}.md` · requirements r5 §6/§7 · stories US-014~022.
**Outputs (Part 2)**: `../findings/functional-design/{business-logic-model.md, business-rules.md, domain-entities.md}`

**Scope**: detailed algorithms + data structures for the findings components. Consumes domain's `Finding`/`Coverage` contract (fixed); does not renegotiate it (except a possible small naming clarification — FQ2).

---

## A. Components to design (from Application Design)
`Normalizer` (US-014, P1) · `Deduplicator` (US-015, P2) · `SeverityMapper` (US-016, P3) · `ComplianceMapper` (US-017/018, P5/P6/P7) · `CoverageAccountant` (US-020/021, P4) · `ReportBuilder` (US-019) · `FindingsStore` (US-022).

## B. Execution checklist
### Part 1 — Planning
- [x] B-1. Component/context analysis (§A)
- [x] B-2. Questions (§C)
- [x] B-3. Collect answers — FQ1=A(CWE소유), FQ1b=A, FQ2=A, FQ3=A, FQ4=B, FQ5=A
- [x] B-4. Step 5 — FQ1 재해석 수용(P6 재정의), FQ1b(no-canonical) 해소. 모호성 없음
- [x] B-5. (질문 완결; 산출물 생성 후 승인)

### Part 2 — Generation
- [x] B-6. `business-logic-model.md` — pipeline data flow, per-component algorithms, P1–P7 realization
- [x] B-7. `business-rules.md` — normalization/dedup/severity/mapping/accounting rules (concrete)
- [x] B-8. `domain-entities.md` — findings-local structures (MappingTable, Report, per-tool severity tables)
- [x] B-9. Update aidlc-state.md

---

## C. 기능 설계 질문 (모든 `[Answer]:` 를 채워 주세요)

> 형식: `[Answer]:` 뒤에 선택지 문자. `X) 기타`는 자유 서술.

### 질문 1 — CoverageAccountant는 PS-ID 상태를 어떻게 판정하나? *(핵심 — P6를 가름)*
커버리지는 **PS-ID 축**이고 finding은 **CWE 기반**입니다. property **P6** 요구사항: *프레임워크 매핑 테이블을 추가·변경해도 커버리지는 비트 단위로 불변.* 따라서 커버리지는 **CWE→PS-ID 투영에서 파생되면 안 됩니다.**

A) **체커→PS-ID 소유권 (AI 권장)** — 각 체커가 자신이 소유한 PS-ID를 선언(레지스트리 `CheckerID → []PSID`, 예: `observation:PS-009 → [PS-009]`, `humble → [PS-001,015,016,017,024]`). 회계 방식: WorkItemOutcome에서 체커가 **실행됨** ⇒ 그 PS-ID는 `Found`(=판정됨); `Failed`에 있으면 ⇒ `ToolFailed`; 위임·not-applicable은 설정에서. **P6가 자명하게 성립** — 회계가 CWE→프레임워크 매핑을 절대 참조하지 않음. CWE→KISA 투영은 오직 리포트에서 **finding을 프레임워크 언어로 라벨링**하는 데만 사용.

B) **CWE→PS-ID 투영** — finding의 CWE를 PS-ID로 투영해 Found 판정. **P6 위반** (매핑 변경이 커버리지를 움직임). ✗

C) 하이브리드 ([Answer]에 서술)

[Answer]: A. 하지만 KISA 점검 항목은 범용성이 떨어지기 때문에 CWE를 소유를 했으면 좋겠음. 내말은 CheckerID -> []CWE. 그렇다면 `observation:CWE-615 → [CWE-615], humble → [CWE-319,614,1004,1275,1021]`이 되겠지. 매핑은 불변이라는 점은 다시 해석하면 점검 CWE는 불변, KISA, OWASP-Top-10같은 점검 항목은 점검 CWE를 근거로 대응.

### 질문 1b — (FQ1 재해석 수용 후) no-canonical 항목 회계 *(Step 5 후속 — 필수)*

FQ1 모델 확정: 체커는 **CWE 소유**(`CheckerID → []CWE`), KISA/OWASP는 CWE→프레임워크 매핑으로 투영. P6는 "프레임워크 추가 시 기존 커버리지 불변"으로 만족(KISA 커버리지는 CWE→KISA 매핑만 읽음).

문제: `no-canonical-cwe` 항목(PS-025 subzy)은 어떤 CWE로도 키잉 불가 → totality·tool-failed 귀속 불가. 공유 sentinel이라 전역 매핑도 틀림.

A) **no-canonical 소수 항목만 명시적 체커→PSID 소유 + 부팅 완전성 검증 (AI 권장)** — 매핑/설정에 `nonCanonicalOwners: { PS-025: subzy }` 같은 소수 예외 선언. 나머지 25개는 CWE→KISA 투영. 부팅 시 검증: `26칸 = (CWE-도달) ∪ (no-canonical 명시소유) ∪ (delegated)` 분할 성립, 아니면 부팅 실패. tool-failed: 체커 실패 시 그 체커의 "책임 PSID 집합"(=투영된 CWE 항목 ∪ 명시 소유)이 tool-failed.

B) PS-025에 대표 CWE를 억지로 지정(예: CWE-1327 등) — no-canonical 원칙(A-3) 위반, 발명 금지 규칙과 충돌 ✗

C) 기타 (자유 서술)

[Answer]: A

### 질문 2 — `StatusFound` 의미와 명칭
domain은 `StatusFound // chiller judged the item` 로 코딩되어 있습니다. 의미: 어떤 항목이 "Found"인 것은 그 항목의 소유 체커가 **실행되어 판정했다**는 뜻이며 — **이슈(finding)가 나왔는지와 무관**합니다. (이슈 0건인 정상 항목도 여전히 `Found`=검사됨.) 실제 이슈는 커버리지와 별개로 findings 목록에 있습니다.

A) **의미 유지 + 명확성을 위해 `StatusFound` → `StatusChecked` 로 리네임 (AI 권장)** — "Found"는 "이슈 발견"으로 읽히지만 실제 의미는 아님; "Checked/Judged"가 domain 주석과 일치. domain 소폭 리네임(+테스트·요약 갱신).

B) `StatusFound` 그대로 유지 (명칭 수용, 문서로 명확히)

C) `StatusFound`가 "이슈 발견"을 뜻해야 함 — 그러면 검사했으나 정상인 항목에 5번째 상태가 필요(4상태 분할 깨짐) ✗

X) 기타

[Answer]: A

### 질문 3 — 매핑 테이블 파일 스키마 (FR-14, Q5=A 선언적 파일)
A) **`cwe` 키 기반 YAML (AI 권장)**:
```yaml
version: "kisa-2026-07-08"
map:
  CWE-540: { KISA: [PS-009], OWASP: [], CAPEC: [] }
  CWE-615: { KISA: [PS-009] }
  no-canonical-cwe: { KISA: [] }   # 명시적: 아무것도 투영 안 함, 침묵 아님 (R8)
```
투영 = 테이블 조회; 없는 프레임워크 키 ⇒ 빈 결과 (R10). MVP는 KISA만 채움.

B) 다른 스키마 ([Answer]에 서술)

X) 기타

[Answer]: A

### 질문 4 — 도구별 severity 표 위치 (R4, US-016)
A) **코드 내, 어댑터당 표 하나, `unknown→SevInfo` 폴백 (AI 권장)** — severity는 컴플라이언스 프레임워크가 아님; 작은 도구별 맵을 SeverityMapper와 함께 둠. 총함수(P3).

B) 설정 파일에 (매핑 테이블처럼) — 런타임 튜닝 가능하나 파일/포맷 추가

X) 기타

[Answer]: B

### 질문 5 — NotApplicable 판정
어떤 PS-ID가 `NotApplicable`(vs `Found`/`ToolFailed`)이 되는 때는?
A) **대상 집합에 구조적으로 적용 불가일 때만 (AI 권장)** — 예: 쿠키 항목(PS-015/016/017)인데 대상이 쿠키를 전혀 설정하지 않는 경우; 로그인폼 항목(PS-003/023)인데 로그인폼이 없는 경우. 소유 체커가 명시적 "N/A" 신호(에러도 finding도 아님)를 반환해 판정. 체커가 실행됐다면 기본값은 `Found`(판정됨).

B) MVP에선 N/A 미사용 — 실행된 항목은 전부 `Found`; 미실행 ⇒ `ToolFailed`. (더 단순; N/A는 Post-MVP)

X) 기타

[Answer]: A

---

## D. 확장 규칙 준수 (Planning)
| Extension | 상태 |
|---|---|
| property-based-testing | **This unit owns P1–P7.** FQ1 fixes P6's structural guarantee; FQ2/FQ5 fix P4's state semantics; FQ3 fixes P5/P7. Detailed per-component property tables produced in Part 2. Blocking 없음 |
| security-baseline | `FindingsStore` bounded-evidence + encryption (US-022, FR-12/SEC-01); redaction (R9). Designed in business-rules. Blocking 없음 |
| resiliency-baseline | `ToolFailed` accounting (FQ1) realizes US-027 at the accounting layer. Blocking 없음 |

---

## 질문 6 — no-canonical 항목의 ATT&CK 분류 (사용자 제안)

PS-025 등 no-canonical 항목은 "약점(CWE)"보다 "적대자 기법(ATT&CK TTP)"에 더 잘 대응(예: 서브도메인 탈취 → T1584.001). "no-canonical-cwe" 공백을 의미 있는 분류로 대체하자는 제안. 다만 ATT&CK는 CWE(약점)와 **다른 축**(적대자 행위)이라 정규 축 교체는 단일-축 모델을 깸.

A) **ATT&CK를 투영 프레임워크로 추가 (정규 축 유지) (AI 권장)** — CWE는 정규/ dedup 축 유지. ATT&CK는 KISA/OWASP/CAPEC 옆 프레임워크(Post-MVP). no-canonical 항목은 **PS-ID 키 예외 맵**(FQ1b 소유 엔트리 확장)으로 ATT&CK 라벨 획득. 회계·domain·P6 무변경. 예외 맵 구조는 지금 설계.

B) **ATT&CK를 no-canonical의 canonical fallback 정체성으로** — Finding 정체성 = CWE ∪ ATT&CK-TTP. no-canonical 항목이 실제 정규 분류 획득. 그러나 domain 이중-축 변경(완료된 U0 재작업), dedup 키 확장, C4(“CWE canonical”) 재해석.

C) 추가 안 함 — no-canonical sentinel 그대로.

X) 기타

[Answer]: A + 네이밍 `nonCanonical` → `attackTechnique` (공격자 관점 축)
