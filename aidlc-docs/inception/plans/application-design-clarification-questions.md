# Application Design — Clarification (Step 8/9 follow-up)

**Trigger**: Step 8 분석 of `application-design-plan.md` §C 답변.
**Blocking**: Part 2(설계 산출물 생성)는 이 파일이 해소될 때까지 진행하지 않음. 그리고 **Q2는 승인된 requirements 변경(r3)을 유발** — 아래 AC7 참조.

---

## Step 8 분석 결과

| Q | 답 | 판정 |
|---|---|---|
| Q1 바이너리 토폴로지 | A | ✅ 확정 — 단일 이미지, `--role=manager\|worker` |
| **Q2 외부 도구 실행** | **ZAP 제거 + 대규모 재편** | 🔴 **requirements 변경 + 공백/충돌** → AC1~AC7 |
| Q3 fan-out | A | ✅ 확정 — WorkQueue 인터페이스 + Redis/Valkey |
| Q4 Checker 인터페이스 | A | ✅ 확정 — 단일 `Checker` + `ScanContext` |
| Q5 매핑 계층 | A | ✅ 확정 — 선언적 버전관리 파일 |
| **Q6 영속성** | **A + 되물음** | ⚠️ Task vs 분배 단위 질문 → AC0에서 답변+확인 |
| Q7 API | A | ✅ 확정 — Platform 전용 `ScanAPI` |

---

## AC0. (Q6 되물음 답변) Task ≠ WorkQueue 분배 단위 — 두 계층입니다

> 질문: "사용자의 분석 요청은 큰 작업일텐데, Task는 WorkQueue로 분배되는 단위랑 같아, 달라?"

**다릅니다. 2계층입니다.**

```
Scan(Task) 애그리거트  ── 1개 스캔 요청(URL 집합 + 옵션). FSM 상태(US-024)를 가짐. TaskStore에 영속.
       │  fan-out (US-026, bounded 동시성)
       ▼
WorkItem 다수  ── 분배 단위. WorkQueue가 운반. 각 WorkItem = (checker × URL-batch).
                 완료 여부 원장(ledger)이 CAS-resume(US-025)의 진실 원천.
```

- **Task(=Scan)** 는 오케스트레이션 애그리거트: 하나의 사용자 분석 요청. FSM 전이(created→running→…)는 Task 수준.
- **WorkItem** 은 그 Task가 팬아웃한 세부 작업: 큐가 분배하고 워커가 집어감. `(체커 × URL 배치)` 단위.
- **CAS-resume(US-025)** 는 WorkItem 수준에서 동작(어떤 item이 끝났나) — 큐가 아니라 **TaskStore의 완료 원장**이 진실 원천. 큐는 분배만.
- coolant 대응: Task 애그리거트 + `current_asynq_id` CAS를 세부 분배 작업에 거는 패턴 그대로(AWS-strip).

즉 `TaskStore`는 **Task 애그리거트 + WorkItem 완료 원장**을 영속하고, `WorkQueue`는 **WorkItem**을 운반합니다. Q6=A(단일 store + repository 인터페이스)는 유효 — Task/Findings/원장이 같은 논리 store의 별도 컬렉션.

### AC0 확인 질문 — WorkItem 분배 입도(granularity)

A) **`(체커 × URL 배치)` (AI 권장)** — 체커별로 URL을 배치로 묶어 분배. 배치 크기 파라미터화(NFR-14). 큐 물량과 단일요청재사용(FR-02, 배치 내 공유 fetch)의 균형.

B) `(체커 × 단일 URL)` — 최소 입도, 재개 정밀하나 큐 물량 폭증

C) `(체커 전체 × 전체 URL)` = 체커 하나가 한 WorkItem — 팬아웃 최소, 부분실패 격리는 체커 단위로만

X) 기타

[Answer]: X. 체커를 "단일 URL마다 체크해야함"/"서비스 글로벌하게 체크해야함"으로 종류를 나누고, (체커 N개 x 단일 URL). 이 방법은 어때.

---

## Q2 심층: ZAP 제거는 옳은 방향이나, 정밀 확정이 필요합니다

ZAP(JVM 상주) 제거 → subprocess CLI 모델 통일은 **NFR-05(복잡도 감소)·NFR-04(cloud-agnostic)와 정합**합니다. 방향 자체는 채택 권장. 다만 ZAP가 담던 **14개 관찰 항목을 재귀속**해야 하고, 답변의 재편 표에 공백·충돌이 있습니다.

### 25개 항목 재매핑 (AI 종합안 — 공백 메움 + 충돌 해소)

| 도구 | 항목 | 비고 |
|---|---|---|
| **testssl.sh** | PS-002 | 불변 |
| **humble** *(신규)* | PS-001, 015, 016, 017, 024 | 헤더+쿠키. ZAP 최대 덩어리 대체 |
| **httpx** *(신규, AC2)* | PS-007, 020 | 서버/프레임워크 버전 |
| **nuclei** *(신규, AC1)* | PS-005, 006, 010, 026 | 정보노출·스택트레이스·.git·open redirect |
| **Corsy** | PS-021 | 불변 |
| **dirhunt** | PS-004 | 불변 *(AC4)* |
| **changeme** | PS-019 | 불변 |
| **subzy** | PS-025 | 불변 (`no-canonical-cwe`) |
| **native (in-process)** | PS-008, 011, 012, 013, 022, 023 | 기존 6종 |
| **native (신규 3종)** | **PS-003, PS-009, PS-018** | ZAP에서 이전 — 평문 로그인폼 / HTML 주석 / URL 세션ID |
| **ASM (delegated)** | PS-014 | 불변 |

**회계 검증**: 5+2+4+1+1+1+1+(6+3)+ (002)1 = **26** ✅ 전 항목 귀속, 공백 0.

**답변 대비 조정 3건**:
1. **PS-003(평문 로그인폼)** — 답변에서 누락됨 → **native 신규**로 메움 (http:// 로 서빙되는 password 필드 폼 탐지, tight anchor).
2. **PS-004(디렉터리 인덱싱)** — 답변이 nuclei와 native 양쪽에 언급 → **dirhunt 유지**(원래 전용 도구, ZAP 제거와 무관). *(AC4에서 확인)*
3. **PS-009(HTML 주석)** — 답변이 "nuclei(정보노출)/native(주석)"로 분할 → 회계는 항목당 1소유자여야 하므로 **native 단독**. *(AC5에서 확인)*

### 도구 분류체계 붕괴 (중요)

ZAP가 사라지면 **"Observation" 체커 계열**의 존재 이유가 소멸합니다(그건 곧 ZAP였음). 3계열(Observation/Probe/Native) → **2계열로 단순화**:

- **Probe (외부 CLI)**: humble · httpx · nuclei · testssl · dirhunt · changeme · Corsy · subzy (8종)
- **Native (in-process 시그니처)**: 9종

Q4=A의 `Checker` + `ScanContext` seam은 그대로 유효 — 오히려 더 깔끔해집니다(공유 응답 소비형 vs 자체 I/O형의 구분만 남음). 이 붕괴는 stories §H-2 Checking 서브도메인 서술도 갱신 대상.

---

## AC1. nuclei의 정체 (독립성 핵심)

승인된 requirements §2.2는 **"No coolant/Nuclei dependency"** 를 창립 결정으로 못박았습니다(chiller 독립의 이유). nuclei를 다시 쓰려면 무엇으로서인지 확정해야 합니다.

A) **번들 CLI 도구 (AI 권장)** — chiller가 **nuclei 바이너리 + 선별된 소수 passive/info-disclosure 템플릿을 이미지에 번들**, subprocess로 실행. 다른 프로브와 동일 취급. **coolant 서비스에 의존하지 않음** → §2.2의 독립성 유지. 템플릿은 이미지에 **정적 번들 + 버전핀**(SECURITY-10)이라, NFR-05가 피하려던 "동적 템플릿 PVC/S3 배포 + Kueue" 복잡도는 **발생하지 않음**.

B) **coolant/Nuclei 서비스 호출** — 런타임 의존. §2.2 창립 결정을 뒤집음. *(비권장 — chiller 독립성 상실)*

X) 기타

[Answer]: nuclei가 필요한 커버리지는 제외

## AC2. 버전 탐지 도구 (PS-007, 020)

A) **httpx (AI 권장)** — ProjectDiscovery Go 단일 바이너리. nuclei와 같은 생태계 → subprocess 모델·버전핀 일관. tech/version 탐지 지원.

B) **whatweb** — Ruby, 핑거프린트 depth는 더 풍부하나 런타임(Ruby) 추가

C) 둘 다

X) 기타

[Answer]: A

## AC3. 신규 native 3종 승격 확인

PS-003(평문 로그인폼) · PS-009(HTML 주석) · PS-018(URL 세션ID)을 native 시그니처로 이관합니다. stories의 §H-4(오탐 위험 항목 개별 스토리 승격) 정책상, 이 3종을 어떻게?

A) **3종 모두 개별 스토리로 승격 (AI 권장)** — 기존 native 6종(US-008~013)과 동일하게 US-030/031/032 신설. tight anchor가 항목마다 달라 개별 수용 기준 필요.

B) 묶어서 1개 "추가 native" 스토리

X) 기타

[Answer]: A

## AC4. PS-004(디렉터리 인덱싱) 소유자

A) **dirhunt 유지 (AI 권장)** — 원래 전용 프로브, ZAP 제거와 무관. 답변의 양쪽 언급은 슬립으로 간주.

B) nuclei로 이관 (dirhunt 제거) — 도구 수 감소하나 전용성 상실

X) 기타

[Answer]: A

## AC5. PS-009(HTML 주석) 소유자

A) **native 단독 (AI 권장)** — 응답 본문 주석 스캔은 tight body-anchor라 chiller native가 소유. 회계 1소유자 원칙 충족.

B) nuclei 단독

X) 기타

[Answer]: A

## AC6. 도구 분류체계 2계열 붕괴 확인

A) **Observation 계열 소멸 → Probe(CLI) + Native 2계열 (AI 권장)** — ZAP 없으니 Observation의 정의가 사라짐. humble/httpx/nuclei는 Probe로. Checker seam 단순화.

B) Observation 계열 유지 — humble/httpx/nuclei를 "관찰형"으로 재정의 (개념만 유지, 실행은 CLI)

X) 기타

[Answer]: X. 내생각엔 Observation 에서 PS-009를 확인하는 native engine이 동작

## AC7. requirements r3 개정 처리 (승인된 산출물 변경)

Q2는 requirements를 변경합니다: §2.1(체커 구성표) · §2.2("No ZAP"는 원래 없었지만 nuclei 독립성 문구 갱신) · **FR-03**(ZAP 기반 관찰 체커 → CLI 관찰 어댑터로 재작성) · **FR-04**(프로브 확장) · §8(traceability) · stories US-006/007 + native 스토리 집합.

A) **지금 requirements를 r3로 개정 후 재승인 (AI 권장)** — FR-14(r2) 때와 동일 절차. 개정본 재승인 → stories 갱신 → 그 다음 Application Design 산출물 생성. 추적성 유지.

B) Application Design 산출물에만 반영, requirements는 나중에 일괄 개정

X) 기타

[Answer]: A

---

## 참고: 이번 라운드 확정 (재확인 불필요)
Q1=A(단일 이미지 role-flag) · Q3=A(WorkQueue 인터페이스+Redis/Valkey) · Q4=A(Checker+ScanContext) · Q5=A(선언적 매핑 파일) · Q6=A(단일 store+repository, AC0에서 Task/WorkItem 2계층 확정) · Q7=A(Platform ScanAPI)

---

# Round 2 — AC0/AC1/AC6(=X) 재분석: 세 답이 하나의 더 깔끔한 설계로 수렴

## Step 9 재분석

| AC | 답 | 판정 |
|---|---|---|
| AC0 WorkItem 입도 | **X** — scope로 체커 분류, `(체커 N개 × 단일 URL)` | 🟢 **개선안 채택** — AC0-A보다 나음 |
| AC1 nuclei 정체 | **"nuclei 필요 커버리지는 제외"** | ⚠️ 재귀속 필요 — 어떤 항목이 진짜 nuclei 필요인가 |
| AC2 버전 도구 | A (httpx) | ✅ 확정 |
| AC3 native 3종 승격 | A (개별 스토리) | ✅ 확정 (단, 항목 수 증가 → BC5) |
| AC4 PS-004 | A (dirhunt) | ✅ 확정 |
| AC5 PS-009 | A (native) | ✅ 확정 |
| AC6 분류체계 | **X** — Observation을 native engine이 수행 | 🟢 **채택** — 2계열, 단 명칭은 Observation 유지 |
| AC7 requirements r3 | A | ✅ 확정 (개정 범위 확대) |

**세 X가 합쳐지는 지점**: AC6-X("Observation = native engine이 동작")는 곧 **"Native"라는 이름을 버리고, in-process 엔진의 계열명을 Observation으로 유지"** 하자는 것. AC1("nuclei 제외")은 nuclei가 담던 항목 중 **native로 관찰 가능한 것(에러노출·스택트레이스·.git)을 그 Observation 엔진으로 흡수**하면 해결됨. AC0-X는 그 체커들을 **URL 단위로 팬아웃**하는 규칙. → 하나의 그림:

### 확정될 체커 분류체계 (2계열, 명칭 Observation 유지)

| 계열 | 구현 | 담당 항목 | 개수 |
|---|---|---|---|
| **Observation** (in-process native engine) | chiller 자체 시그니처 엔진 | PS-003·005·006·008·009·010·011·012·013·018·022·023 | 12 |
| **Probe** (외부 CLI subprocess) | humble·httpx·testssl·dirhunt·changeme·Corsy·subzy | 001·015·016·017·024(humble) · 007·020(httpx) · 002(testssl) · 004(dirhunt) · 019(changeme) · 021(Corsy) · 025(subzy) | 12 |
| **Delegated** | 외부 스테이지 | 014→ASM · **026→?(BC1)** | 2 |

**회계: 12 + 12 + 2 = 26** ✅. nuclei 완전 제거. "Native"→"Observation"으로 명칭 통일.

### WorkItem scope 모델 (AC0-X 채택)

체커를 **동작 범위**로 분류하고, 그에 따라 WorkItem 입도를 정함:

| scope | 정의 | WorkItem | 소속 체커 |
|---|---|---|---|
| **URL-scoped** | 단일 URL의 취득 응답을 판정 | `(단일 URL × 모든 URL-scoped 체커)` — URL 1회 fetch 후 공유(FR-02) | Observation 12종 전부 · humble · httpx · Corsy · changeme |
| **Host-scoped** | host/서비스당 1회 (URL 수 무관) | `(단일 host × 해당 host-scoped 체커)` | testssl(host:port) · dirhunt(사이트) · subzy(도메인) |

→ FR-02(단일요청재사용)가 URL-scoped WorkItem 정의에 자연 내장됨. 부분실패 격리(US-027)는 WorkItem 단위.

---

## BC1. PS-026 (Open Redirect) 처리 — nuclei 제외의 유일한 실질 공백

nuclei가 담던 4항목 중 **005·006·010은 native 관찰 가능**(에러/스택트레이스 본문 시그니처, .git은 .env(PS-011)와 동일 패턴) → Observation 엔진으로 흡수. 문제는 **PS-026 Open Redirect** 하나 — 이건 redirect 파라미터를 넣고 Location 반사를 봐야 해서 **순수 관찰이 아니라 능동에 가깝습니다**(ZAP도 passive가 아닌 active 룰로 잡음). 애초에 "observation"으로 분류된 게 무리였습니다.

A) **Active-Scanner로 위임 (AI 권장)** — PS-014→ASM처럼 PS-026→AI-Active-Scanner로 `delegated`. chiller의 "passive만, 능동공격 금지"(§2.2, C-2) 원칙에 정직. 커버리지 회계엔 `delegated`로 남아 갭이 아님. → chiller 24 커버 + 2 위임.

B) **경량 native 프로브로 유지** — redirect 파라미터 1개만 살짝 테스트하는 native 체크. 원칙(passive)과 회색지대.

C) **완전 제외 (drop)** — 26개에서 빼고 25개 체계로. 회계 denominator 변경.

X) 기타

[Answer]: X. 외부의 nuclei engine이 커버리지 할 것.

## BC2. PS-005·006·010의 Observation(native) 흡수 확인

A) **확인 — native Observation 엔진으로 흡수 (AI 권장)** — PS-005(에러페이지 정보노출)·006(스택트레이스)은 응답 본문 시그니처, PS-010(.git 노출)은 PS-011(.env)과 동일한 well-known-path 체크. 셋 다 tight-anchor라 chiller native가 자연 소유. nuclei 불필요.

B) 이 셋도 제외 — "nuclei가 담던 것은 전부 제외"로 해석

X) 기타

[Answer]: A. 근데 에러페이지 정보 노출은 에러를 발생시켜야 하는데 Observation 엔진으로 가능?

## BC3. 분류체계 명칭 확인 (AC6-X 구체화)

A) **Observation(in-process native engine) + Probe(external CLI) 2계열 (AI 권장)** — "Native"란 명칭을 버리고 in-process 엔진을 **Observation**으로 부름. humble/httpx 등 외부 도구는 Probe. Checker seam(Q4=A)은 그대로.

B) 3계열 유지 (Observation/Probe/Native) — 개념 3개 유지, 그러나 ZAP 없는 Observation과 Native의 실질 구분이 모호

X) 기타

[Answer]: A

## BC4. WorkItem scope 모델 확인

A) **URL-scoped / Host-scoped 2분류 + 위 표의 체커 배정 (AI 권장)** — URL-scoped WorkItem = `(단일 URL × 모든 URL-scoped 체커)`, Host-scoped WorkItem = `(단일 host × host-scoped 체커)`. 각 도구의 scope 배정은 위 표 기준(정밀 배정은 Functional Design에서 핀).

B) 전부 URL-scoped로 통일 — host-scoped 도구(testssl/dirhunt/subzy)도 대표 URL로 취급 (중복 실행 위험)

X) 기타

[Answer]: A

## BC5. Observation(native) 스토리 집합 확대 처리

Observation 엔진이 12항목으로 커졌습니다(기존 native 6 + 003/009/018 + 005/006/010). AC3=A(개별 스토리 승격) 정책을 12항목 전부에 적용할까요?

A) **12항목 전부 개별 스토리 (US-008~013 + US-030~035) (AI 권장)** — 전부 tight-anchor라 항목별 수용 기준 필요. §H-4 정책 일관.

B) **본문 시그니처류만 개별, well-known-path류는 묶음** — 예: 005/006/009(본문)은 개별, 008/010/011/012/022(well-known path)는 "경로 존재 확인" 1~2 스토리로 묶음

X) 기타

[Answer]: A

---

## 참고: Round 2에서 확정 (재확인 불필요)
AC2=A(httpx) · AC3=A(개별 승격, 수량은 BC5) · AC4=A(dirhunt) · AC5=A(PS-009 native) · AC7=A(requirements r3 개정 — 범위: nuclei 제외 + Observation 명칭 + 026 위임 + WorkItem scope 모델)

---

# Round 3 — passive 경계 확정 (BC1/BC2 후속)

## Step 9 재분석 (Round 2)
| BC | 답 | 판정 |
|---|---|---|
| BC1 PS-026 | **X** — 외부 nuclei engine이 커버 | 🟢 채택 = **PS-014→ASM과 동형의 파이프라인 위임 (PS-026→Nuclei 스테이지)**. DC1에서 위임 의미론 확인 |
| BC2 005/006/010 흡수 | **A + 지적** ("에러 유발 필요") | ⚠️ 정당한 지적 — 010은 순수관찰(OK), **005/006은 에러 유발 필요** → DC2 |
| BC3 명칭 | A | ✅ Observation(native)+Probe(external) |
| BC4 WorkItem scope | A | ✅ URL/host-scoped |
| BC5 스토리 개별화 | A | ✅ (수량은 DC2 결과 반영) |

## DC1. PS-026 위임 의미론 확인
"외부 nuclei engine이 커버"를 다음으로 해석했습니다 — 맞나요?

A) **파이프라인 수준 위임 (PS-014→ASM과 동형) (AI 권장)** — chiller는 PS-026을 판정하지 않고 리포트에 `delegated → Nuclei stage`로 표기. 실제 커버는 형제 Nuclei 스테이지(coolant/Service-Nuclei-Worker)가 독립 수행, 결과는 **상위 파이프라인이 병합**(chiller가 nuclei를 호출하지 않음). → §2.2 독립성 유지. `delegated`는 대상 라벨(`ASM` / `Nuclei`)을 가짐(FR-11/13 데이터 모델 소폭 확장).

B) chiller가 런타임에 외부 nuclei 서비스를 호출해 결과 수집 — 런타임 의존 발생 (§2.2 완화 필요)

X) 기타

[Answer]: A

## DC2. PS-005·006 (에러페이지·스택트레이스) 처리 — BC2 지적 해소
010(.git)은 순수 관찰이라 Observation native 확정. 005/006은 에러를 유발해야 관찰됩니다.

A) **005/006도 Nuclei 스테이지로 위임 (AI 권장)** — 에러 유발은 관찰이 아니라 fault injection에 가까움(특히 006 스택트레이스는 서버 예외 유발 필요). 026과 동일 논리로 Nuclei 위임 → **chiller는 순수 passive 유지**. 결과: Observation native **10종**(003·008·009·010·011·012·013·018·022·023), Delegated **4종**(014→ASM · 005·006·026→Nuclei). 회계 22 covered + 4 delegated = 26.

B) **005만 native(양성 404 프로브로 기본 에러페이지 관찰), 006은 Nuclei 위임** — 404 유발은 `.env` GET과 동급의 benign. 006 스택트레이스만 위임. Observation 11종, Delegated 3종.

C) **005/006 모두 native 유지** — benign 404/오류 프로브로 관찰. chiller 자급자족하나 passive 경계 회색.

X) 기타

[Answer]: A

---
## 참고: Round 3에서 확정 (재확인 불필요)
BC3=A · BC4=A · BC5=A · PS-010 = Observation native 확정 · nuclei는 chiller에 미포함(위임만)

---

# Round 4 — 사용자 제안: 프록시 기반 ResponseCache (Valkey)

**제안 (사용자)**: Probe 단계에서 프록시로 HTML/JS 등 웹페이지 데이터·응답값을 key-value(Valkey)에 캐싱 → Observation N개 항목 점검을 병렬화.

**AI 평가 요약**:
- ✅ **진짜 이득**: fetch를 **도구 계열 경계 너머로 재사용** (httpx가 받은 HTML을 Observation·humble이 재요청 없이 소비). FR-02를 "family 내부"→"family 전체"로 격상, 대상 호스트 부하 감소(NFR-13, P6 보호).
- ⚠️ **과대평가 주의**: "N개 병렬" 자체 이득은 작음 — 관찰 체크는 싸고 이미 in-process goroutine 병렬. 캐시가 여는 건 워커 간 분산인데 실익 대비 왕복 오버헤드.
- 🔴 **비용**: 캐시 대상이 HTML/JS 원본 바디 = 민감정보 포함. **FR-12(바디 persist 금지)·SECURITY-01/03과 충돌** → 반드시 휘발성·암호화·TTL·스캔종료 wipe로 제약.

## EC1. ResponseCache 채택 여부
A) **채택 — Application Design 컴포넌트 `ResponseCache`(Valkey-backed) 신설 (AI 권장)** — Fetcher가 fetch 결과를 캐시에 기록, HTML-fetching 체커가 캐시에서 읽음. 명분 = cross-family fetch 재사용 + 대상 부하 감소(NFR-13). Valkey는 이미 WorkQueue(Q3=A)로 스택에 있음(동일/별도 인스턴스는 Infra Design).

B) 미채택 — FR-02는 기존대로 워커 메모리 내 공유로만 (프로브 자체 fetch 중복은 감수)

X) 기타

[Answer]: A. HAR 포멧이 이 정보를 저장할 수 있는가?

## EC2. FR-12 / SECURITY 경계 (충돌 해소)
A) **휘발성 스캔-스코프 캐시로 정의 (AI 권장)** — ResponseCache는 **findings 저장소가 아님**. 제약: (1) 스캔-스코프 TTL, 스캔 종료 시 wipe; (2) at-rest 암호화(SECURITY-01); (3) bounded max-body-size; (4) 접근통제. requirements r3에 "FR-12의 no-persisted-bodies는 findings 영속 대상이며, 휘발성 암호화 스캔 캐시는 허용된 작업상태"를 명시(FR-12 clarification + 신규 NFR).

B) 캐시도 FR-12 대상 — 바디 캐싱 금지, 해시/메타만 캐싱 (fetch 재사용 이득 대부분 상실)

X) 기타

[Answer]: A

## EC3. 캐시 사용 범위 (어떤 도구가 캐시를 쓰나)
A) **HTML-fetching URL-scoped 체커만 (AI 권장)** — Observation 엔진 + humble + httpx가 캐시 read/write. **TLS/DNS/디렉터리 도구(testssl·subzy·dirhunt)는 HTML을 안 받으므로 캐시 우회** (자체 프로토콜 I/O). Corsy·changeme는 엔드포인트별 특수 요청이라 부분 사용(Functional Design에서 핀).

B) 모든 아웃바운드를 프록시 경유 — testssl(TLS handshake)·subzy(DNS)까지 프록시. 이득 없는 곳에 복잡도만 추가

X) 기타

[Answer]: A

## EC4. WorkItem 입도 재조정 (병렬화 목표)
캐시 도입 시 "URL 1회 fetch → 캐시 → 체크들"의 fetch/check 분리가 가능해집니다.
A) **fetch/check 분리, 단 check는 기본 in-process 병렬 유지 (AI 권장)** — WorkItem은 여전히 URL-scoped(`URL × 모든 URL-scoped 체커`)이되, 내부적으로 fetch→캐시 후 체크들을 goroutine 병렬. 워커 간 분산은 "동일 URL 재등장/대용량 시" 옵션. 단순성 유지 + fetch 재사용 확보.

B) `(URL × 단일 체크)`로 분해해 워커 간 완전 분산 — 최대 병렬이나 큐 물량↑·캐시 왕복↑ (실익 낮음, AI 비권장)

X) 기타

[Answer]: A

---
## 참고: 이 라운드는 requirements r3 승인 전 선결 (캐시 결정이 r3 FR-02/FR-12 문구에 반영됨)

---

# Round 5 — JS 소스 점검 스코프 (사용자 질문: F12 Sources의 JS도 점검 대상인가 / HAR 저장 가능한가)

**AI 사실 답변**:
- HAR는 네트워크로 받은 `.js`를 엔트리별로 저장 가능(`response.content.text`); 인라인 스크립트는 HTML 바디에 포함. ✅
- 단 chiller는 GET만 하고 JS를 **실행하지 않음** → F12 Sources(런타임 뷰)의 동적 주입/eval JS는 못 봄. chiller가 보는 JS ⊆ F12.
- chiller는 `<script src>`를 파싱해 JS를 **자동 발견하지 않음**(크롤링=상위 담당). 상위 크롤러가 준 `.js` URL만 검사 가능.

## FC1. JS **내용** 점검을 스코프에 넣을까? (KISA 26에 없는 신규 관심사)
"JS 안의 시크릿/API키/내부 엔드포인트/주석 민감정보"는 26개 항목에 별도로 없음. 가장 가까운 PS-009는 "HTML 주석".

A) **MVP 스코프 밖 — Post-MVP 관찰 시그니처로 예약 (AI 권장)** — chiller 코어는 KISA 26 유지. JS-secret 스캔은 ResponseCache/HAR에 JS가 이미 있으니 값싸게 추가 가능한 **Post-MVP Observation 시그니처 클래스**로 백로그에 명시(신규 CWE-540/200 계열, PS 항목 아님). 지금 26 커버 미션을 늦추지 않음.

B) **PS-009를 "클라이언트 전달 소스의 민감정보"로 확장** — HTML 주석 + JS 주석/시크릿까지 PS-009 하나로. 정의 확장이라 requirements r4 개정 필요. (KISA 원 정의와 어긋날 수 있음)

C) **지금 신규 항목으로 추가** — "JS 시크릿 노출"을 27번째 점검으로 정식 추가. 미션 범위(26) 확대 + requirements r4.

D) **완전 범위 밖** — JS 내용은 chiller가 다루지 않음(관심사 아님).

X) 기타

[Answer]: B

## FC2. JS 발견·렌더링 경계 확인
A) **현행 유지 (AI 권장)** — chiller는 상위가 준 URL(‌`.js` 포함 가능)만 GET·검사. HTML의 `<script src>` 자동 발견 ❌, 헤드리스 렌더링 ❌ (passive·no-crawling 전제 유지, FR-01/§2.2).

B) 헤드리스 렌더링 도입 — F12 parity(동적 JS까지). passive/no-crawling 전제를 깨는 대형 능력 추가 → 별도 requirements 논의 필요.

C) `<script src>` 파싱해 JS URL만 추출·검사 (렌더링 없이 "얕은 발견") — no-crawling 경계 회색지대.

X) 기타

[Answer]: B

---
## 참고
FC 결과가 스코프 확장(B/C 계열)이면 requirements r4 개정 후 재승인. A/D면 Application Design 승인 그대로 진행 (필요시 Post-MVP 백로그 한 줄 추가).

---

# Round 6 — FC1=B/FC2=B 화해 (헤드리스 렌더링의 충돌 해소)

## Step 8/9 분석
| FC | 답 | 판정 |
|---|---|---|
| FC1 | **B** — PS-009를 "클라이언트 전달 소스(HTML 주석 + JS 주석/시크릿)"로 확장 | ⚠️ 정의 확장 → requirements r4. 합리적(HAR에 JS 존재) |
| FC2 | **B** — 헤드리스 렌더링 도입(F12 parity) | 🔴 FR-01/02/10·NFR-05·보안과 충돌. ZAP 제거(경량화) 방향 역행 |

## GC1. FC2 접근 재확정 (핵심)
헤드리스 렌더링은 무겁고(Chromium > ZAP) 여러 승인 요구사항과 충돌. 어떻게?

A) **경량 정적 발견으로 전환 (구 FC2=C) (AI 권장)** — HTML의 `<script src>`를 파싱해 **그 JS URL들을 GET**(실행 없음). 브라우저 없이 정적 외부 JS 확보. 순수 동적 주입 JS만 놓침. FR-01/02/10·NFR-05 대체로 유지, `<script src>` 추출만 "얕은 발견"으로 허용.

B) **하이브리드: 코어 26은 경량 GET 유지, 헤드리스는 JS-점검 확장에만 + Post-MVP** — 26 항목 경로는 그대로. 헤드리스 렌더링은 오직 동적 JS 시크릿 점검용으로 격리하고 Post-MVP. 무게를 필요한 곳에만.

C) **전면 헤드리스 렌더링 (원 FC2=B 유지)** — 최대 parity, 최대 비용. GC2/GC3 화해 필수. requirements r4 대폭 개정.

X) 기타

[Answer]: A. 놓치는 순수 동적 주입 JS는 어떤 경우에서 발생함?

## GC2. (GC1=B/C 선택 시) 서드파티 sub-resource vs FR-10
렌더링/발견이 CDN·서드파티 호스트 JS를 부름 → 승인 목록 밖 호스트 요청.
A) **승인 호스트(same-origin + allowlist)만 fetch, 그 외 차단+로깅 (AI 권장)** — FR-10 fail-closed 유지. 서드파티 CDN JS는 미점검(부분 커버).
B) 모든 sub-resource 허용 — FR-10 완화 (비권장, P6 위험).
C) N/A (GC1=A이고 script src도 승인 호스트만)

X) 기타

[Answer]: C

## GC3. PS-009(FC1=B) 확장 시점·CWE
A) **MVP: HTML 주석 + 정적 fetch된 JS 주석/시크릿; 동적 JS는 Post-MVP (AI 권장)** — CWE 확장 CWE-615(주석)+CWE-540(소스 내 정보)+CWE-200. requirements r4에 PS-009 재정의 + Post-MVP 백로그(동적 JS).
B) MVP에 동적 JS까지 포함 — GC1=C(렌더링) 필요. r4 대폭.
C) FC1도 전부 Post-MVP로 미룸 — 지금 r4 없이 백로그만

X) 기타

[Answer]: A

---
## 참고
GC 결과가 스코프/데이터모델 변경이면 requirements **r4** 개정 후 재승인 → 그 다음 Application Design 산출물 보정(렌더링/발견 시 Fetcher·ResponseCache·ApprovalGate 상호작용) → 승인 → Units Generation.

---

# Round 7 — 동적 JS의 Post-MVP 경로 (actionable 재프레이밍)

## GC1=A 후속 답변: "놓치는 순수 동적 주입 JS는 어떤 경우?"
`<script src>` GET + 인라인이 놓치는 것 = ① 코드스플릿 청크(lazy import) ② 로더/태그매니저 주입(GTM 등) ③ 런타임 eval/생성 코드 ④ 상호작용/로그인 후 로드.
**Actionable 수율**: ①만 높음(1st-party 시크릿), ②③은 낮음(3rd-party/eval), ④는 중간·고비용.
**핵심**: ①(actionable)은 **헤드리스 없이 정적 열거 가능** — webpack manifest/`asset-manifest.json`/엔트리 번들의 청크-URL 매핑 파싱. 순수 동적-only JS = 대부분 non-actionable. SPA-aware 크롤러면 청크 URL이 이미 상위에서 공급됨.

## GC4. 동적 JS Post-MVP 경로 확정 (FC2=B 뒤집기)
A) **Post-MVP = 정적 manifest/청크 열거, 헤드리스 완전 폐기 (AI 권장)** — 1st-party 청크 시크릿을 브라우저 없이. 순수 동적/3rd-party/eval JS는 의도적 비대상(낮은 actionable). 비용·보안·FR-10 충돌 회피.

B) Post-MVP에 헤드리스 렌더링 유지(옵션) — 완전 F12 parity 원할 때. 비용/보안/FR-10 부담 인지.

C) 동적 JS 완전 폐기 — MVP 정적 JS(GC3=A)까지만. Post-MVP 백로그도 없음.

X) 기타

[Answer]: A

---
## 확정 요약 (GC4 답변 후 requirements r4로)
GC1=A(정적 `<script src>` 발견) · GC2=C(승인 호스트만) · GC3=A(PS-009 MVP=HTML+정적 JS, CWE-615/540/200) · GC4=? (동적 JS Post-MVP 경로)

---

# Round 8 — 동적 JS 발견 재검토 (정적 매니페스트 열거의 번들러 의존 한계)

**트리거(사용자 지적)**: GC4=A(정적 매니페스트/청크 열거)는 **번들러 결합**. `asset-manifest.json` 파싱은 webpack/CRA 전제 — Vite·esbuild·Next.js·난독화·매니페스트 비노출 환경에서 청크-URL 정적 복원 불가 → actionable한 1st-party 청크를 놓침. 미문서화 시 감사(FSI 43 "앱 소스코드 내 운영정보 노출")에서 커버리지 공백 지적 가능.

**핵심 재프레이밍(사용자)**: 이 공백을 실제로 막는 건 Playwright 헤드리스뿐이나, **산출물은 DOM이 아니라 렌더 중 실제 요청된 JS URL(네트워크 로그, `page.on("request"/"response")`)** 이어야 함 — DOM은 자산이 아니고 JS 본문 시크릿은 DOM에 안 남음. URL 캡처 → GET → 시크릿 스캔. 번들러 무관 완전 발견. 비용: passive/no-execution 전제 이탈 + 브라우저 상주(GC1에서 헤드리스 접은 이유와 충돌 → "자산 발견 목적"으로 재도입 결정 필요).

**domain 유닛 무영향**: `ScanContext.DiscoveredJS []*HAREntry`는 발견 방법과 독립. 이 결정은 requirements A-6/scan Fetcher에만 영향.

## HC0 (확정, 질문 아님): MVP known gap 문서화
목표 수준과 무관하게, MVP(`<script src>` 정적 발견만)는 동적 JS를 못 봄 → requirements에 **명시적 known limitation**으로 기록(감사 방어). 어느 답이든 반영.

## HC1. Post-MVP 동적 JS 발견 목표 수준
A) **완전 발견 → Playwright 네트워크 캡처** — 헤드리스 재도입("자산 발견 목적" 한정: depth-0 렌더 + network log의 JS URL 수집 + 승인호스트 GET + 시크릿 스캔; DOM 덤프 아님, interaction/navigation 없음). 번들러 무관 완전 발견. 비용: passive/no-execution 이탈 + 브라우저 상주. *완전성이 컴플라이언스 의무일 때.*

B) **실용 확보 + gap 문서화** — 정적 매니페스트/청크 열거 유지, 번들러 의존 한계·놓치는 청크를 known gap으로 명시. 가볍고 passive 유지. *비용 회피 우선.*

C) **하이브리드 (AI 권장)** — 기본 정적 열거; 매니페스트 비노출/청크 미발견 감지 시 그 대상만 **opt-in Playwright 자산 발견 모드**로 승격. 무거운 헤드리스를 필요한 대상에만. FSI 43 근거 + webpack/CRA 무료 경로 둘 다 살림.

X) 기타

[Answer]: A

---
## 반영 계획 (HC1 답변 후)
- HC0: requirements에 MVP 동적-JS known gap 명시 (무조건).
- HC1=A/C: A-6 개정 → Playwright "자산 발견(네트워크 캡처)" 모드(승인호스트, depth-0, DOM 아님) Post-MVP; 신규 NFR/constraint; scan Fetcher에 반영(해당 유닛 Construction 시). requirements r5.
- HC1=B: A-6에 번들러 의존 한계 + known gap 명시. requirements r5(경량).
- domain FD 승인은 이 결정과 독립 — 별도로 대기 중.
