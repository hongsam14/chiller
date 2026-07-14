# Story Planning — Clarification Questions (Round 2)

**Stage**: INCEPTION → User Stories (Part 1: Planning, Step 10 — mandatory follow-up)
**Trigger**: Step 9 ambiguity analysis of `story-generation-plan.md` §F answers.
**Blocking**: Part 2 (스토리/페르소나 생성) 는 이 파일의 모든 `[Answer]:` 가 채워질 때까지 진행하지 않습니다.

---

## Step 9 분석 결과 요약

| Q | 답변 | 판정 | 사유 |
|---|---|---|---|
| Q1 | B | ✅ 확정 | 명확 |
| Q2 | A | ✅ 확정 | 명확 |
| **Q3** | **A + "내부적으로는 C를 같이 쓸거 같음"** | ⚠️ **옵션 결합** | 6개 도메인과 3개 광역 도메인의 **관계와 적용 계층**이 미정의 |
| **Q4** | **A** | ⚠️ **미정의 기준** | "까다로운 항목 … 등" — 개별 스토리로 승격할 항목의 **확정 목록**이 없음 |
| **Q5** | **C** | ⚠️ **Q1과 충돌** | Q1=B는 시스템 액터(P5)를 페르소나에서 제외 → "시스템 액터에는 Job Story"의 **적용 대상이 존재하지 않음** |
| Q6 | A | ✅ 확정 | 명확 |
| Q7 | A | ✅ 확정 | 명확 |
| Q8 | A | ✅ 확정 | 명확 |
| Q9 | A | ✅ 확정 | 명확 |
| **Q10** | **A+C 혼합 + CWE 정규화/다중 컴플라이언스 매핑** | 🔴 **신규 요구사항 (스코프 변경)** | 승인된 `requirements.md` §6 Finding 모델과 §2 미션 범위를 **변경**함. 1차 지표 우선순위도 미정. |

---

## C1. (Q3 후속) 6개 도메인과 3개 광역 도메인의 관계

Q3에서 A(6개: Ingestion / Checking / Normalization / Orchestration / Reporting / Safety)를 고르시면서 "내부적으로는 C(3개: Scan / Findings / Platform)를 같이 쓸거 같다"고 하셨습니다. 두 축이 **어느 계층에 각각 적용되는지** 결정 규칙이 필요합니다.

A) **2계층 구조**: 3개 광역 도메인이 **상위 bounded context**, 6개가 그 **하위 도메인**. 스토리 에픽은 6개 하위 도메인 기준, Go 패키지 최상위는 3개 광역 컨텍스트.
매핑: `Scan{Ingestion, Checking, Safety}` · `Findings{Normalization, Reporting}` · `Platform{Orchestration}`
**(AI 권장: Safety가 Scan 컨텍스트 안에 있으면서도 독립 하위 도메인으로 남아 SECURITY-11의 "보안 핵심 로직 격리"가 유지됩니다)**

B) **2계층 구조, 다른 매핑**: `Scan{Ingestion, Checking}` · `Findings{Normalization, Reporting}` · `Platform{Orchestration, Safety}` — Safety를 플랫폼 횡단 관심사로 취급

C) **1계층 + 태그**: 에픽은 6개 도메인 그대로, 3개 광역 도메인은 스토리에 붙는 **분류 태그**로만 사용 (패키지 구조에는 강제하지 않음)

D) **Safety는 횡단(cross-cutting)**: 6개가 아니라 5개 도메인 + Safety를 모든 도메인에 걸친 정책 계층으로 (Go에서는 미들웨어/데코레이터)

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: A

---

## C2. (Q5 후속) Job Story 적용 대상

Q5=C("고전형 + 시스템 액터에는 Job Story 혼용")를 고르셨으나, Q1=B는 시스템 액터 P5(상위 크롤러 / 하위 소비자)를 **정식 페르소나에서 제외**했습니다. 현재 상태로는 Job Story를 적용할 액터가 없습니다. 어느 쪽으로 정리할까요?

A) **상태 의존 시나리오에 Job Story 적용** — 액터가 아니라 *상황*을 기준으로. 구체 규칙: **부분 실패, 재개(resume), 롤백, 레이트 리밋 트립** 처럼 트리거/상태가 본질인 스토리는 Job Story(`When <상황>, I want to <동기>, so I can <기대 결과>`), 나머지는 고전형.
**(AI 권장: Q5=C의 원래 의도 — "상태 의존 시나리오에 유리하다"는 B 옵션의 설명 — 를 살리면서 Q1=B와 충돌하지 않습니다)**

B) **Q1을 C로 변경** — P5(시스템 액터)를 정식 페르소나로 승격하고, P5의 스토리만 Job Story로 작성

C) **Q5를 A로 변경** — 전부 고전형. 상태 의존 시나리오는 Given/When/Then 수용 기준(Q6=A)이 이미 상황을 표현하므로 충분

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: A

---

## C3. (Q4 후속) 개별 스토리로 승격할 "오탐 위험" 항목 확정

Q4=A는 "까다로운 항목(PS-013, PS-023 **등**)만 개별 스토리로 승격"입니다. "등"을 확정 목록으로 바꿔야 합니다. 네이티브 시그니처 6종(FR-05)이 오탐 위험이 가장 큽니다.

A) **네이티브 6종 전부 개별 스토리** — PS-008(robots.txt), PS-011(.env), PS-012(백업파일), PS-013(관리자 페이지), PS-022(crossdomain.xml), PS-023(CAPTCHA 부재). 나머지 19개는 체커 단위로 묶음.
**(AI 권장: FR-05의 "anchors MUST be tight" 는 항목마다 앵커가 다르므로 항목별 수용 기준이 필요합니다. 6개면 백로그 소음이 되지 않습니다)**

B) **판정이 "부재 증명"인 항목만** — PS-013(관리자 페이지 공개 접근), PS-023(CAPTCHA 부재) 2개. 나머지 네이티브 4종은 문자열 앵커가 명확하므로 묶음.

C) **네이티브 6종 + 프로브 중 PS-025(subzy, 서브도메인 탈취)** — 탈취 판정의 오탐 위험이 큼

D) 승격 없음 — 전부 체커 단위 스토리, 오탐 기준은 수용 기준 안에서 항목별로 열거

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: A

---

## C4. (Q10 후속 — 핵심) Finding의 **정규 식별자(canonical identity)** 는 무엇입니까?

"Coverage는 결국 CWE로 표기된다"는 답변은 데이터 모델의 근간을 건드립니다. 승인된 `requirements.md` §6은 Finding을 `ps_id` 중심으로 정의합니다. 어느 쪽이 1차 키인지에 따라 엔진 전체 구조가 달라집니다.

A) **CWE가 정규(canonical), PS-ID는 파생** — 체커는 finding을 CWE로 방출하고, 컴플라이언스 매핑 계층이 CWE → KISA PS-ID / OWASP Top 10 / 기타 프레임워크로 투영. 새 컴플라이언스 체계 추가 시 매핑 테이블만 추가.
**(AI 권장: "CWE를 KISA 항목, OWASP Top 10 등등의 컴플라이언스에 대입할 수 있게" 라는 서술의 직역이며, 프레임워크 추가가 체커를 건드리지 않습니다. 대신 CWE→PS-ID 매핑이 1:N/N:1이라 §C5 해소가 필수)**

B) **PS-ID가 정규, CWE는 부가 속성** — finding은 지금처럼 PS-ID로 판정하고 `cwe_id` 필드를 덧붙임. OWASP 매핑은 PS-ID→CWE→OWASP 2단 조회.

C) **둘 다 1급, 매핑 테이블이 진실의 원천** — finding은 `(source_tool, rule_id)` 로 방출되고, 매핑 테이블이 `rule_id → CWE → {KISA PS-ID, OWASP A0x, …}` 를 모두 관장. 체커는 컴플라이언스 체계를 전혀 모름.

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: A. 그리고 점검항목은 CAPEC(Common AttackPattern)에 더 대응이 쉬울 수 있음.

---

## C5. (Q10 후속) 매핑 카디널리티와 대상 프레임워크 범위

CWE ↔ KISA PS-ID 는 1:1이 아닙니다. 하나의 PS 항목이 여러 CWE에 걸치고(예: PS-013 관리자 페이지 공개 ≈ CWE-284 + CWE-306), 하나의 CWE가 여러 PS 항목을 커버하기도 합니다. 또 일부 항목은 대응 CWE가 애매합니다(예: PS-025 서브도메인 탈취).

**(a) 카디널리티 규칙**

A) **N:M 허용** — finding 하나가 복수 CWE를, CWE 하나가 복수 PS-ID를 가질 수 있음. 커버리지 회계(FR-13)는 **PS-ID 축**에서 수행 (26개 총합 불변식 유지).
**(AI 권장: FR-13의 `covered ∪ not-applicable ∪ delegated = 26` 불변식과 PBT property를 깨뜨리지 않는 유일한 안전한 선택입니다. CWE는 표현/집계 축, PS-ID는 회계 축)**

B) **1:1 강제** — 각 PS-ID에 대표 CWE를 하나씩 지정. 단순하지만 표현력 손실.

C) **N:M 허용 + 커버리지 회계도 CWE 축에서** — "CWE 커버리지" 를 1차 지표로 (전체 CWE 목록 정의 필요 → 범위 폭증 위험)

X) 기타

[Answer]: A

**(b) 지금 범위에 포함할 컴플라이언스 프레임워크**

A) **KISA 26항목만 지금 구현, CWE 필드는 채우되 매핑 계층은 확장 가능하게 설계** (OWASP Top 10 등은 `Post-MVP` 태그)
**(AI 권장: Q8=A의 MVP/Post-MVP 태깅과 정합. 지금 OWASP 매핑까지 구현하면 25개 항목 커버라는 본래 미션이 늦어집니다)**

B) **KISA + OWASP Top 10 둘 다 지금 구현**

C) **KISA + OWASP Top 10 + 추가 프레임워크(구체적으로 [Answer]에 명시)**

X) 기타

[Answer]: A

---

## C6. (Q10 후속) 승인된 `requirements.md` 개정 처리

CWE/컴플라이언스 매핑은 **새로운 기능 요구사항**입니다 (§6 Finding 모델 + 신규 FR + 신규 PBT property + 미션 범위 §2). AI-DLC 규칙상 승인된 산출물의 변경은 명시적으로 처리되어야 합니다.

A) **지금 `requirements.md`를 개정** — §6에 `cwe_id`/매핑 추가, **FR-14 (컴플라이언스 프레임워크 매핑)** 신설, §7에 매핑 관련 PBT property 추가, §2 미션 문구 갱신. 개정본을 재승인 받은 뒤 스토리 생성.
**(AI 권장: 스토리가 requirements를 역추적(E-6)하는 구조이므로, requirements에 없는 요구사항을 스토리가 커버하면 추적성이 깨집니다. 개정은 국소적입니다)**

B) **스토리에만 반영하고 requirements는 Workflow Planning 이후 일괄 개정**

C) **CWE 전체를 Post-MVP로 미룸** — 지금은 PS-ID 기반으로 진행, CWE는 후속 이터레이션

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: A

---

## C7. (Q10 후속) 1차 성공 지표 확정

"A와 C의 혼합"이라 하셨습니다. 두 지표가 **동시에 1차**일 수는 없습니다 (트레이드오프가 실재합니다 — 커버리지 완전성을 높이면 스캔 시간이 늘어납니다). 우선순위와 C의 측정 정의가 필요합니다.

**(a) 1차 지표**

A) **커버리지 회계 완전성이 1차, 처리량은 제약(constraint)** — "모든 스캔에서 26개 PS-ID가 covered/not-applicable/delegated 중 하나로 분류된다"가 성공의 정의이고, 처리량은 "스캔이 N시간 안에 끝나야 한다"는 상한 제약으로 표현.
**(AI 권장: 이 프로젝트의 존재 이유가 커버리지 갭 해소이고, 혼합 지표는 측정 불가능합니다. 하나는 목표, 하나는 제약으로 두는 것이 표준입니다)**

B) 처리량이 1차, 커버리지가 제약

C) 두 지표를 각각 별도 스토리의 수용 기준으로 분리하고, 우선순위는 부여하지 않음

X) 기타

[Answer]: A

**(b) 처리량/소요 시간 지표의 측정 정의** — C를 제약으로 쓰려면 숫자가 필요합니다. 현시점에 목표치가 있으신가요?

A) **아직 없음 — NFR Requirements 단계에서 확정.** 스토리에는 "설정 가능한 상한 내에 완료" 로만 표현
**(AI 권장: 지금 숫자를 지어내면 근거 없는 제약이 됩니다. NFR-14(스케일)가 이미 이 논의를 예약해 두었습니다)**

B) 목표치 있음 — [Answer]에 명시 (예: "URL 1,000개 기준 30분 이내")

X) 기타

[Answer]: A

---

## 참고: 이 라운드에서 확정된 사항 (재확인 불필요)

- **페르소나**: P1 컴플라이언스 담당자 · P2 보안 분석가 · P3 플랫폼/SRE 운영자 · P4 파이프라인 통합자 · P6 대상 시스템 소유자 (Q1=B)
- **분해 방식**: 도메인 에픽 × 저니 순서 × 페르소나 태그 (Q2=A)
- **수용 기준**: Given/When/Then, `Then` 절이 PBT property oracle로 승격 (Q6=A)
- **부분 실패**: 독립 1급 스토리로 승격, `found`/`not-applicable`/`delegated`/`tool-failed` 4상태 확정 (Q7=A)
- **범위**: requirements 전체 커버 + 스토리별 `MVP`/`Post-MVP` 태그 (Q8=A)
- **ID 체계**: `US-001…` + `Traces:` 필드 + 말미 FR→스토리 역방향 커버리지 표 (Q9=A)

---

# Round 3 — 단일 잔여 질문

## Step 9 재분석 (Round 2 답변)

| C | 답변 | 판정 |
|---|---|---|
| C1 | A | ✅ 확정 — 2계층: `Scan{Ingestion, Checking, Safety}` · `Findings{Normalization, Reporting}` · `Platform{Orchestration}` |
| C2 | A | ✅ 확정 — Job Story는 *상황* 기준 (부분 실패/재개/롤백/레이트리밋 트립), 나머지 고전형 |
| C3 | A | ✅ 확정 — 네이티브 6종(PS-008/011/012/013/022/023) 개별 스토리 승격 |
| **C4** | **A + "점검항목은 CAPEC에 더 대응이 쉬울 수 있음"** | ⚠️ **추측형 서술** — CAPEC의 지위가 미정 (정규 축인가, 파생 투영인가, 범위 밖인가) |
| C5(a) | A | ✅ 확정 — N:M 허용, 커버리지 회계는 PS-ID 축 |
| C5(b) | A | ✅ 확정 — KISA만 지금 구현, 매핑 계층은 확장 가능하게 |
| C6 | A | ✅ 확정 — `requirements.md` 지금 개정 후 재승인 |
| C7(a) | A | ✅ 확정 — 커버리지 완전성 = 목표, 처리량 = 제약 |
| C7(b) | A | ✅ 확정 — 처리량 목표치는 NFR Requirements에서 확정 |

---

## C8. (C4 후속) CAPEC의 지위

C4=A(CWE 정규)를 고르시면서 "점검항목은 CAPEC(Common Attack Pattern)에 더 대응이 쉬울 수 있음"이라 덧붙이셨습니다. 이는 **가능성 서술**이므로 결정이 필요합니다.

**분석 (AI):**

1. **CWE 대응은 실제로 25/26이 깔끔합니다.** PS-004→CWE-548, PS-005/006→CWE-209, PS-009→CWE-615, PS-010→CWE-527, PS-012→CWE-530, PS-013→CWE-306, PS-014→CWE-749, PS-015→CWE-614, PS-016→CWE-1004, PS-017→CWE-1275, PS-018→CWE-598, PS-021/022→CWE-942, PS-024→CWE-1021, PS-026→CWE-601 … 유일한 난점은 **PS-025 Subdomain Takeover**(대응 CWE 불명확). *(지시적 매핑 — 확정은 Functional Design)*
2. **CAPEC 대응은 오히려 어렵습니다.** CAPEC은 *공격 패턴*이지 *약점*이 아닙니다. "HSTS 헤더 부재"는 공격 패턴이 아니라, 공격(CAPEC-102 Session Sidejacking)을 *가능하게 하는 약점*입니다. chiller는 **관찰로 약점을 판정**하는 passive 스캐너이므로 방출물은 weakness입니다. CAPEC은 **AI-Active-Scanner**에 자연스러운 축입니다.
3. **OWASP Top 10 공식 매핑은 CWE 위에 정의**됩니다 (각 OWASP 카테고리 = CWE 집합). "CWE를 OWASP Top 10에 대입" (Q10 답변)은 **CWE가 정규일 때만** 성립합니다. CAPEC→OWASP 공식 매핑은 존재하지 않습니다.
4. **CAPEC은 CWE에서 파생 가능**합니다 (CAPEC 항목의 `Related_Weaknesses` 필드). 따라서 CAPEC은 새 정규 축이 아니라 매핑 계층의 **또 하나의 투영 대상** — KISA/OWASP와 동일 지위.

**질문:** CAPEC을 어떻게 위치시킬까요?

A) **파생 투영 (Post-MVP)** — CWE가 정규(C4=A 유지). FR-14 매핑 계층의 대상 프레임워크를 `{KISA PS-ID, OWASP Top 10, CAPEC}` 로 두되, MVP는 KISA만 구현(C5b=A). CAPEC은 MITRE 공식 `CAPEC↔CWE` 관계로 파생.
**(AI 권장: 원래 목표(OWASP 대입)를 살리고, 새 정규 축을 만들지 않으며, CAPEC을 버리지도 않습니다)**

B) **CAPEC을 정규 축으로 변경** — 체커가 CAPEC ID로 finding을 방출하고 CWE/KISA/OWASP를 파생. *(주의: OWASP 공식 매핑이 끊기고, "관찰된 약점"을 "공격 패턴"으로 표기하게 됩니다)*

C) **CWE·CAPEC 이중 정규** — finding이 두 축을 모두 1급으로 보유

D) **CAPEC 범위 밖** — 지금도, 나중에도 다루지 않음. FR-14 대상은 `{KISA PS-ID, OWASP Top 10}` 만

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: A
