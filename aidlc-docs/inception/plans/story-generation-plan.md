# Story Generation Plan — chiller (passive-scanner engine)

**Stage**: INCEPTION → User Stories (Part 1: Planning)
**Role**: Product Owner
**Date**: 2026-07-08
**Inputs**: `../requirements/requirements.md` (approved) · `user-stories-assessment.md` (Execute = Yes) · `../reverse-engineering/` (coolant, reference only)
**Outputs (Part 2)**: `../user-stories/stories.md` · `../user-stories/personas.md`

---

## A. 방법론 (Methodology)

스토리는 **요구사항(FR/NFR/SECURITY/PBT)을 액터 중심의 검증 가능한 결과물로 번역**하는 것을 목표로 합니다.
Requirements가 "시스템이 무엇을 해야 하는가"를 규정했다면, 스토리는 "누가, 왜, 무엇을 관찰할 수 있어야 하는가"를 규정합니다.

원칙:
1. 모든 스토리는 **INVEST** (Independent, Negotiable, Valuable, Estimable, Small, Testable) 를 만족한다.
2. 모든 스토리는 **최소 1개의 FR 또는 NFR로 역추적(traceability)** 된다. 역으로, 모든 FR은 최소 1개 스토리로 커버된다.
3. 수용 기준(Acceptance Criteria)은 **관찰 가능한 결과**로 서술한다. "타이트하게 매칭한다" 같은 의도 표현은 금지 — "X 픽스처에서 finding 0건" 처럼 판정 가능해야 한다.
4. PBT(FULL 모드)가 활성화되어 있으므로, requirements §7의 property 시드(커버리지 총합, dedup 멱등성, severity 전사성)는 해당 스토리의 수용 기준에 명시적으로 앵커링한다.
5. 보안/복원력 확장 규칙 중 스토리 단계에 적용 가능한 항목(SECURITY-05/08/10/11/15, NFR-08/12/13, FR-08/13)은 수용 기준으로 흡수한다.

---

## B. 후보 페르소나 (질문 Q1에서 확정)

| # | 후보 | 성격 | 주요 관심사 |
|---|---|---|---|
| P1 | **컴플라이언스 담당자** | 인간, 리포트 소비자 | 26개 PS-ID 전체 회계, `delegated`(PS-014) vs `gap` 구분, 감사 증적 |
| P2 | **보안 분석가** | 인간, finding 트리아지 | 오탐 억제, 증거(evidence) 스니펫의 충분성, severity 신뢰도 |
| P3 | **플랫폼/SRE 운영자** | 인간, 엔진 운영 | 스캔 상태·진행률, 부분 실패 가시성, 재개(resume), 배포/롤백, 관측성 |
| P4 | **파이프라인 통합자** | 인간(외부 시스템 소유) | API 계약 안정성 — 외부 비통제 소비자 존재(Q3=A), 버저닝 |
| P5 | **상위 크롤러 / 하위 소비자** | 시스템 액터 | URL 목록 투입, 리포트 수령 |
| P6 | **대상 시스템 소유자** | 인간, *비사용자 이해관계자* | 사전 승인되지 않은 스캔 거부, 레이트 리밋 — 피해자 관점(scanner-as-DoS) |

---

## C. 스토리 분해 방식 옵션 (질문 Q2에서 확정)

| 방식 | 설명 | 이 프로젝트에서의 장점 | 단점 |
|---|---|---|---|
| **User Journey-Based** | 사용자 흐름(스캔 요청 → 실행 → 상태 조회 → 리포트 소비) 순서로 배열 | 엔드투엔드 흐름이 명확, 통합 테스트와 잘 맞음 | 3개 체커의 이질성이 한 스토리에 뭉개질 수 있음 |
| **Feature-Based** | 시스템 기능(체커, 정규화, FSM, API) 단위 | FR과 1:1에 가까움 | 사용자 가치가 흐려짐 (INVEST의 V 약화) |
| **Persona-Based** | 페르소나별로 묶음 | 이해관계자 소통에 최적 | 동일 기능이 여러 페르소나에 중복 등장 |
| **Domain-Based** | 도메인 경계(Ingestion / Checking / Normalization / Orchestration / Reporting / Safety) 단위 | NFR-01의 domain-driven 패키지 레이아웃과 정렬 → Application Design으로 자연 이행 | 도메인 경계를 먼저 확정해야 함 |
| **Epic-Based** | 계층형 에픽 → 하위 스토리 | 25개 항목 규모를 감당, 백로그 관리 용이 | 에픽 축을 무엇으로 잡을지 다시 결정 필요 |
| **Hybrid (AI 권장)** | **에픽 축 = 도메인**, 각 에픽 내부는 **저니 순서**로 스토리 배열, 각 스토리에 **페르소나 태그** | 도메인 경계가 곧 Go 패키지 경계(NFR-01)가 되고, 저니 순서가 통합 테스트 시나리오가 되며, 페르소나 태그로 이해관계자 리뷰 가능 — 세 목적을 모두 충족 | 초기 구성 비용이 조금 큼 |

---

## D. 실행 체크리스트 (Part 1 — Planning)

- [x] D-1. User Stories 필요성 평가 및 문서화 (`user-stories-assessment.md`) — 결정: 실행
- [x] D-2. 후보 페르소나 도출 (섹션 B)
- [x] D-3. 스토리 분해 방식 옵션 정리 및 트레이드오프 제시 (섹션 C)
- [x] D-4. 명확화 질문 작성 (섹션 F, `[Answer]:` 태그 임베드)
- [x] D-5. 사용자가 섹션 F의 모든 `[Answer]:` 태그를 채움 (Q1=B, Q2=A, Q3=A+C, Q4=A, Q5=C, Q6=A, Q7=A, Q8=A, Q9=A, Q10=A+C+CWE)
- [x] D-6. 답변 모호성 분석 (Step 9) — Q3(옵션 결합) / Q4(미정의 목록) / Q5(Q1과 충돌) / Q10(신규 요구사항 + 혼합 지표) 발견 → `story-planning-clarification-questions.md` 생성
- [x] D-6b. clarification 라운드 답변 수집 및 재분석 — R2(C1~C7=A) + R3(C8=A). **모든 모호성 해소** (섹션 H)
- [x] D-6c. `requirements.md` r2 개정 (C6=A: FR-14/NFR-15 신설, §2/§6/§7/§8/§9/§11 개정) — **2026-07-08 사용자 승인 완료**
- [x] D-7. 사용자로부터 본 계획의 명시적 승인 획득 — **2026-07-08 "Approve & Continue"**

## E. 실행 체크리스트 (Part 2 — Generation, 승인 후 수행)

- [x] E-1. `../user-stories/personas.md` 생성 — 확정된 페르소나의 원형(archetype), 목표, 좌절 지점, 성공 기준
- [x] E-2. `../user-stories/stories.md` 생성 — 승인된 분해 방식에 따라 스토리 작성
- [x] E-3. 모든 스토리가 INVEST 6개 기준을 만족하는지 검증 (스토리별 체크)
- [x] E-4. 모든 스토리에 수용 기준(Acceptance Criteria) 포함 — 승인된 형식으로
- [x] E-5. 페르소나 ↔ 스토리 매핑 표 작성
- [x] E-6. 역추적 표 작성: 스토리 ↔ FR/NFR/SECURITY/PS-ID (모든 FR이 최소 1개 스토리로 커버됨을 증명)
- [x] E-7. PBT property 시드(requirements §7)를 해당 스토리 수용 기준에 앵커링
- [x] E-8. 확장 규칙 준수 요약(security-baseline / resiliency-baseline / property-based-testing) 작성
- [x] E-9. 각 단계 완료 즉시 위 체크박스 `[x]` 표시 + `aidlc-state.md` 갱신

---

## F. 명확화 질문 (모든 `[Answer]:` 태그를 채워 주세요)

> 답변 방법: 각 질문 아래 `[Answer]:` 뒤에 선택지 문자(예: `A`)를 적어 주세요.
> `X) 기타`를 고르면 `[Answer]:` 뒤에 자유롭게 서술해 주세요. 여러 개를 고르려면 `A, C` 처럼 적어 주세요.
> 모든 질문이 채워져야 다음 단계로 진행합니다. (감사 추적을 위해 답변은 이 문서에 남습니다.)

### Question 1
섹션 B의 후보 페르소나 중 어디까지를 `personas.md`에 정식 페르소나로 포함할까요?

A) P1~P4 (인간 액터만) — 시스템 액터(P5)와 비사용자 이해관계자(P6)는 스토리 본문에서 제약으로만 다룸

B) P1~P4 + P6 (인간 액터 + 대상 시스템 소유자) — 사전 승인 게이트/레이트 리밋을 "피해자 관점" 스토리로 명시 **(AI 권장: FR-10과 SECURITY-11의 남용 사례가 주인 없는 요구사항으로 떠다니는 것을 막습니다)**

C) P1~P6 전부 — 시스템 액터도 정식 페르소나로 (기계 대 기계 계약을 스토리화)

D) P1~P3만 — 외부 API 소비자(P4)는 별도 계약 문서로 다루고 스토리에서 제외

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: B

### Question 2
스토리 분해 방식(섹션 C)은 무엇으로 할까요?

A) Hybrid — 에픽 축 = 도메인, 에픽 내부는 저니 순서, 스토리마다 페르소나 태그 **(AI 권장: 도메인 경계가 그대로 Go 패키지 경계(NFR-01)로 이어지고, 저니 순서가 통합 테스트 시나리오가 됩니다)**

B) User Journey-Based — 순수 저니 흐름 중심

C) Domain-Based — 순수 도메인 경계 중심

D) Persona-Based — 페르소나별 묶음

E) Epic-Based — 에픽 축을 체커 종류(Observation / Probe / Native)로 잡음

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: A

### Question 3
Q2에서 도메인 축을 쓴다면, 도메인 경계를 어떻게 잡을까요? (Q2가 B/D면 `N/A`로 답해 주세요)

A) Ingestion / Checking / Normalization / Orchestration / Reporting / Safety — 6개 도메인 **(AI 권장: Safety(사전승인·스로틀링)를 독립 도메인으로 분리하면 SECURITY-11의 "보안 핵심 로직은 전용 모듈에 격리" 요구를 스토리 단계에서 강제합니다)**

B) Ingestion / Checking / Normalization / Orchestration / Reporting — 5개 (Safety는 Checking/Ingestion에 흡수)

C) Scan(요청~실행) / Findings(정규화~리포트) / Platform(FSM·큐·관측성) — 3개 광역 도메인

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: A. 하지만 A의 내부적으로는 C를 같이 쓸거 같음.

### Question 4
스토리 세분화(granularity) 기준은?

A) 하나의 스토리 = 하나의 관찰 가능한 사용자 결과. 25개 PS 항목은 개별 스토리로 쪼개지 **않고** 체커 단위로 묶되, 까다로운 항목(PS-013 관리자 페이지, PS-023 CAPTCHA 부재 등 오탐 위험 항목)만 개별 스토리로 승격 **(AI 권장: 25개를 다 쪼개면 25개의 사실상 동일한 스토리가 생겨 INVEST의 S가 아니라 백로그 소음이 됩니다)**

B) PS 항목 1개 = 스토리 1개 (총 25개 + 인프라 스토리)

C) 체커 1개 = 스토리 1개 (총 3개 체커 스토리 + 나머지)

D) FR 1개 = 스토리 1개 (총 13개 + NFR 스토리)

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: A

### Question 5
스토리 서술 형식은?

A) 고전형 — `As a <페르소나>, I want <기능>, so that <가치>` **(AI 권장: 팀 전반의 인지 비용이 가장 낮습니다)**

B) Job Story — `When <상황>, I want to <동기>, so I can <기대 결과>` (상황/트리거 강조 — 부분 실패·재개 같은 상태 의존 시나리오에 유리)

C) 고전형 + 시스템 액터에는 Job Story 혼용

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: C

### Question 6
수용 기준(Acceptance Criteria) 형식은?

A) Given/When/Then (Gherkin 스타일) **(AI 권장: PBT FULL 모드가 활성화되어 있어 Then 절이 property의 oracle로 그대로 승격됩니다. 예: "Then covered ∪ not-applicable ∪ delegated = 26개 PS-ID 전체")**

B) 체크리스트 불릿 — 간결하지만 상태 전제(Given)가 흐려짐

C) Given/When/Then + 스토리별 "Property" 섹션 별도 추가 (PBT 앵커를 수용 기준과 분리해 명시)

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: A

### Question 7
"부분 실패" 시나리오(FR-08 + FR-13: 한 도구가 죽어도 스캔 전체는 실패하지 않고, 리포트는 26개 항목 회계를 유지)를 스토리에서 어떻게 다룰까요?

A) 독립된 1급 스토리로 승격 — "운영자로서 어떤 체커가 실패했고 어떤 PS-ID가 미판정 상태인지 리포트에서 구분할 수 있다" **(AI 권장: 이 프로젝트에서 가장 오구현되기 쉬운 지점이며, `found`/`not-applicable`/`delegated`/`tool-failed` 4상태 구분이 여기서 확정됩니다)**

B) 각 체커 스토리의 수용 기준 안에 예외 케이스로 포함

C) 별도의 "복원력" 에픽으로 묶어 여러 스토리로 전개

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]:A

### Question 8
스토리 범위를 MVP로 좁힐까요, 아니면 승인된 requirements 전체를 커버할까요?

A) requirements 전체 커버 (FR-01~13 + 적용 가능한 NFR/SECURITY) — 단, 스토리별로 `MVP` / `Post-MVP` 태그를 부여해 우선순위 신호만 남김 **(AI 권장: 커버리지 역추적(E-6)을 온전히 유지하면서도 Workflow Planning에서 단계를 자를 수 있습니다)**

B) MVP만 — 3개 체커 + 정규화 + 리포트. FSM/재개, 블루그린, Warm Standby 관련 스토리는 후속으로 미룸

C) 전체 커버, 우선순위 태그 없음 — 우선순위는 Workflow Planning에서 결정

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: A

### Question 9
스토리 ID 체계와 역추적(traceability) 표기는?

A) `US-001…` 순번 + 각 스토리에 `Traces: FR-06, NFR-03, PS-008` 형태의 역추적 필드, 그리고 문서 말미에 FR→스토리 역방향 커버리지 표 **(AI 권장: requirements §8의 PS-ID 추적 표와 같은 관례를 유지합니다)**

B) 도메인 접두사 + 순번 (`ING-01`, `CHK-01`, `NRM-01` …) + 역추적 필드

C) 에픽 계층 반영 (`E1-S1`, `E1-S2` …)

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: A

### Question 10
비즈니스 성공 지표(성공을 무엇으로 측정하는지)를 스토리 문서에 포함할까요? 포함한다면 무엇이 1차 지표인가요?

A) 포함 — 1차 지표는 **KISA 26개 항목 커버리지 회계의 완전성** (모든 스캔에서 26개가 covered/not-applicable/delegated 중 하나로 분류됨) **(AI 권장: 이 프로젝트의 존재 이유가 "커버리지 갭 해소"이며, FR-13과 직결되어 측정 가능합니다)**

B) 포함 — 1차 지표는 **오탐률(false positive rate)** (분석가가 폐기하는 finding 비율)

C) 포함 — 1차 지표는 **스캔 처리량/소요 시간** (URL 인벤토리 대비)

D) 포함하지 않음 — 성공 지표는 NFR Requirements 단계에서 다룸

X) 기타 (아래 [Answer]: 뒤에 서술)

[Answer]: A와 C의 혼합. 하지만 A에서 Coverage는 결국 CWE로 표기되고, CWE를 KISA 항목, OWASP Top 10 등등의 컴플라이언스에 대입할 수 있게.

---

## G. 확장 규칙 준수 요약 (Planning 단계)

| Extension | 이 단계에서의 상태 |
|---|---|
| **security-baseline** | 적용 가능 규칙을 스토리 수용 기준으로 흡수하도록 계획에 명시 (Q1의 P6 페르소나 = SECURITY-11 남용 사례, Q3의 Safety 도메인 = "보안 핵심 로직 격리", E-8 준수 요약). Blocking finding 없음 — 본 산출물은 계획 문서이며 코드가 아님. |
| **resiliency-baseline** | FR-08/FR-13/NFR-12의 부분 실패·우아한 성능 저하를 Q7로 명시적 결정 대상화하고 E-4/E-6에서 수용 기준·역추적으로 강제. Blocking finding 없음. |
| **property-based-testing** | PBT-01(속성 식별)을 E-7 단계로 계획에 포함, Q6에서 수용 기준 형식이 property oracle로 승격되도록 옵션 설계. 단위별 상세 property 표는 Functional Design에서 산출. Blocking finding 없음. |

---

## H. 확정 결정 (Q1~Q10 + C1~C8) — Part 2 실행 규격

이 섹션이 Part 2 생성의 **유일한 권위 있는 규격**입니다. 섹션 B/C/F의 옵션 나열은 이제 이력(history)일 뿐입니다.

### H-1. 페르소나 (Q1=B)
`personas.md`에 5명 수록. 시스템 액터 P5는 페르소나로 만들지 않고 스토리 본문의 제약으로만 등장.

| ID | 페르소나 |
|---|---|
| P1 | 컴플라이언스 담당자 |
| P2 | 보안 분석가 |
| P3 | 플랫폼/SRE 운영자 |
| P4 | 파이프라인 통합자 (외부 비통제 소비자 소유) |
| P6 | 대상 시스템 소유자 (*비사용자 이해관계자* — scanner-as-DoS 피해자 관점) |

### H-2. 구조 (Q2=A, C1=A)
**2계층.** 스토리 에픽 = 하위 도메인 6개. Go 패키지 최상위 = 상위 bounded context 3개.

```
Scan      { Ingestion, Checking, Safety }
Findings  { Normalization, Reporting }
Platform  { Orchestration }
```

에픽 내부는 **저니 순서**로 스토리 배열. 각 스토리에 **페르소나 태그**.
`Safety`는 `Scan` 컨텍스트 안의 독립 하위 도메인으로 유지 → SECURITY-11 "보안 핵심 로직 격리" 충족.

### H-3. 스토리 서술 형식 (Q5=C, C2=A)
- 기본: 고전형 `As a <페르소나>, I want <기능>, so that <가치>`
- **예외 (Job Story `When <상황>, I want to <동기>, so I can <기대 결과>`)** — 트리거/상태가 본질인 스토리에 한정. 적용 대상 4종: **부분 실패 · 재개(resume) · 롤백 · 레이트 리밋 트립**
- 판정 규칙: 스토리의 주어가 *액터의 의도*면 고전형, *시스템의 상태 전이*면 Job Story

### H-4. 세분화 (Q4=A, C3=A) *(r3 갱신 — ZAP/Nuclei 제거)*
- 원칙: 스토리 1개 = 관찰 가능한 결과 1개
- **개별 스토리로 승격 (10개)**: Observation native 시그니처 전부 — 기존 6종 US-008~013(PS-008/011/012/013/022/023) + r3 신규 4종 US-030~033(PS-003/009/010/018). 사유: FR-05 "anchors MUST be tight"는 항목마다 앵커가 달라 항목별 수용 기준이 필요
- 프로브 12항목(외부 CLI 7도구): US-007 프레임워크 스토리로 묶음
- **delegated 4종**(PS-014→ASM, PS-005/006/026→Nuclei): US-021로 표기 (chiller 미판정)

### H-5. 수용 기준 (Q6=A)
Given/When/Then. `Then` 절은 PBT property의 oracle로 그대로 승격 가능하도록 서술 (requirements §7 참조).
의도 표현 금지 — "타이트하게 매칭한다" ✗ / "픽스처 `benign-robots.txt`에서 finding 0건" ✓

### H-6. 부분 실패 (Q7=A)
독립 1급 스토리로 승격. 4상태 확정: `found` · `not-applicable` · `delegated`(PS-014→ASM) · `tool-failed`.
`tool-failed`는 requirements r2 §6/FR-13에 반영 완료.

### H-7. 범위와 태그 (Q8=A, C5b=A)
requirements r2 전체 커버 (FR-01~14 + 적용 가능한 NFR/SECURITY). 스토리별 `MVP` / `Post-MVP` 태그.
- `MVP`: KISA PS-ID 프레임워크만 구현
- `Post-MVP`: OWASP Top 10 · CAPEC 매핑 테이블

### H-8. ID·역추적 (Q9=A)
`US-001…` 순번. 각 스토리에 `Traces: FR-06, NFR-03, PS-008` 필드. 문서 말미에 **FR → 스토리 역방향 커버리지 표** (모든 FR이 ≥1 스토리로 커버됨을 증명).

### H-9. 성공 지표 (C7=A/A)
- **목표**: 커버리지 회계 완전성 — 모든 스캔에서 26개 PS-ID가 4상태 중 하나로 분류
- **제약**: 처리량/소요 시간 — 스토리에는 "설정 가능한 상한 내 완료"로만 표현, 수치는 NFR Requirements(NFR-14)에서 확정

### H-10. 분류체계 (C4=A, C5a=A, C8=A) — requirements r2 FR-14/NFR-15
- **CWE = 표현 축(정규)**, **PS-ID = 회계 축**. 체커는 CWE만 방출하고 프레임워크를 모른다.
- 매핑은 **N:M** 양방향. 커버리지 회계는 **PS-ID 축**에서만 수행.
- CAPEC은 새 정규 축이 아니라 매핑 계층의 투영 대상 (`Post-MVP`).
- 깔끔한 CWE가 없는 항목(PS-025)은 **`no-canonical-cwe` 마커**를 기록 — ID를 발명하지 않는다.
- 관련 스토리는 requirements §7의 신규 property 3종(투영 totality · projection-does-not-create-coverage · N:M round-trip)을 수용 기준에 앵커링.
