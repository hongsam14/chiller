# Application Design Plan — chiller (passive-scanner engine)

**Stage**: INCEPTION → Application Design · **Date**: 2026-07-08
**Inputs**: `../requirements/requirements.md` (r2) · `../user-stories/stories.md` (29) · `../user-stories/personas.md` · `../reverse-engineering/` (coolant patterns)
**Outputs (Step 10)**: `../application-design/{components,component-methods,services,component-dependency,application-design}.md`

**Scope reminder**: Application Design = **high-level 컴포넌트 식별 + 인터페이스 + 서비스 오케스트레이션 + 의존성**. 상세 비즈니스 로직(dedup 알고리즘, 앵커 정규식 등)은 **Functional Design(CONSTRUCTION, 유닛별)** 로 이연.

---

## A. 컨텍스트 분석 (Step 1)

이미 확정된 아키텍처 골격 (재논의 대상 아님):
- 3 bounded context: `Scan{Ingestion,Checking,Safety}` · `Findings{Normalization,Reporting}` · `Platform{Orchestration}` (§H-2, C1=A)
- **control plane(Platform) → data plane(Scan/Findings) 단방향 의존** (stories 아키텍처 원칙, NFR-02)
- **CWE 정규 축, PS-ID 회계 축**, N:M 매핑 (r2 FR-14)
- coolant *패턴* 복사: Task FSM + CAS-resume + fan-out + OTel (코드/런타임 의존 없음, AWS-strip)
- Go + DI + domain 패키지 + **interface 강제 생성자** (NFR-01)

이 단계가 **처음으로 확정**해야 할 것 (지금까지 의도만 있던 것):
1. **바이너리 토폴로지** (A-1 — 명시적으로 이 단계로 이연됨)
2. 3종 체커(Observation/Probe/Native)를 담는 **Checker 인터페이스의 실제 형태** — 셋의 실행 방식이 이질적(ZAP 데몬 vs CLI 프로브 vs 인라인 함수)
3. **외부 도구 실행 방식** (7개 프로세스를 어떻게 구동하나)
4. **fan-out 메커니즘** (외부 큐 vs in-process pool) — WorkQueue 컴포넌트 존재 여부를 가름
5. **매핑 계층의 형태** (데이터 파일 vs 코드 vs DB)
6. **영속성 경계** (단일 store vs 분리)
7. **API 소유·버저닝** 위치

---

## B. 후보 컴포넌트 인벤토리 (질문으로 확정될 초안)

> 이 표는 질문의 **접지(grounding)** 용 초안입니다. §C 답변으로 경계가 조정됩니다.

### Platform (control plane)
| 컴포넌트 | 책임 | 관련 스토리 |
|---|---|---|
| `ScanOrchestrator` (service) | FSM 구동, 파이프라인 시퀀싱, 데이터 플레인 호출 | US-024, US-026, US-027 |
| `TaskStore` | Scan/Task 영속 + FSM 상태 + CAS-resume | US-024, US-025 |
| `WorkQueue`/`Dispatcher` | fan-out, bounded 동시성 | US-026 |
| `ProgressReporter` | 체커별 진행률, OTel, 헬스체크 | US-028 |
| `ScanAPI` (httpapi) | 스캔 요청/상태/리포트 API, authz | US-001, US-023 |

### Scan (data plane)
| 컴포넌트 | 책임 | 관련 스토리 |
|---|---|---|
| `URLIngestor` | URL 목록 접수, 입력 검증 | US-001, US-003 |
| `ApprovalGate` | 사전 승인 게이트 (fail-closed) | US-004 |
| `RateLimiter` | 대상별 아웃바운드 스로틀 | US-005 |
| `Fetcher` | **단일 요청 재사용** — 응답 1회 취득 후 ResponseCache에 기록 | US-002 |
| `ResponseCache` *(r3, EC1-4)* | Valkey-backed 휘발성 캐시 (HAR 엔트리, 스캔-스코프 TTL, 암호화, wipe). HTML-fetching 체커가 read. FR-02 cross-family 재사용 | US-002 · NFR-16 |
| `Checker` (interface) + registry | 3종 체커 플러그인 seam (NFR-02) | US-006~013 |
| ├ `ObservationChecker` | ZAP passive 어댑터 (14항목) | US-006 |
| ├ `ProbeAdapter` ×5 | testssl/dirhunt/changeme/Corsy/subzy | US-007 |
| └ `NativeChecker` ×6 | PS-008/011/012/013/022/023 | US-008~013 |

### Findings (data plane)
| 컴포넌트 | 책임 | 관련 스토리 |
|---|---|---|
| `Normalizer` | 도구 출력 → CWE-keyed Finding | US-014 |
| `Deduplicator` | finding 중복 제거 (멱등) | US-015 |
| `SeverityMapper` | severity 전사(총함수) | US-016 |
| `ComplianceMapper` | **CWE → PS-ID 투영** (FR-14 매핑 계층) | US-017, US-018 |
| `CoverageAccountant` | 26 PS-ID 4상태 회계 | US-020, US-021 |
| `ReportBuilder` | 통합 리포트 조립 | US-019 |
| `FindingsStore` | finding 영속 (bounded evidence, 암호화) | US-022 |

---

## C. 설계 질문 (모든 `[Answer]:` 를 채워 주세요)

> 형식: `[Answer]:` 뒤에 선택지 문자. `X) 기타`는 자유 서술. 복수 선택은 `A, C`.

### Question 1 — 바이너리 토폴로지 (A-1, 이 단계 확정 대상)
chiller를 몇 개의 실행 단위로 배포할까요?

A) **단일 바이너리, 역할 선택형** — 하나의 이미지를 `--role=manager|worker` (또는 env)로 기동. manager는 API+FSM 오케스트레이션, worker는 체커 실행. 워커만 수평 확장. **(AI 권장: coolant의 manager/worker 분리가 주는 확장성을 유지하면서, 단일 아티팩트로 NFR-05(복잡도 감소)·cloud-agnostic(어디서나 동일 이미지)에 부합)**

B) **2개 별도 바이너리** (manager / worker) — coolant와 동일. 빌드·이미지가 둘로 나뉨

C) **단일 프로세스 (분리 워커 없음)** — 가장 단순, in-process 동시성만. MVP엔 충분할 수 있으나 Warm-Standby/수평확장 여력 축소

X) 기타

[Answer]: A

### Question 2 — 외부 도구 실행 방식
7개 외부 프로세스(ZAP + 프로브 5 — testssl.sh/dirhunt/changeme/Corsy/subzy)를 어떻게 구동할까요?

A) **혼합 (AI 권장)** — ZAP는 **장기 실행 데몬**(worker가 API로 연결), 프로브 5종·네이티브는 **worker 이미지에 번들된 버전핀 바이너리를 in-process subprocess exec**. 사유: ZAP는 본래 데몬, 프로브는 단발 CLI라 실행 수명이 다름. K8s Job보다 단순(NFR-05)하고 standalone 실행 가능(NFR-04). SECURITY-10(버전핀)은 이미지 빌드에서 강제.

B) **K8s Job per invocation** — coolant JobRunner 패턴. 강한 격리, 그러나 K8s 종속·복잡도↑ (cloud-agnostic엔 K8s 필요)

C) **전부 사이드카 서비스** — 각 도구를 상시 서비스로. 리소스 상시 점유

X) 기타

[Answer]: zap을 제거. ZAP를 빼면 passive 커버리지가 이렇게 재편됨. humble → 헤더 + 쿠키 (PS-001·015·016·017·024) — ZAP passive의 가장 큰 덩어리를 이 하나가 대체. whatweb/httpx → 버전 노출 (PS-007·020), Nuclei (기존) → 정보노출·디렉토리인덱싱·.git·open redirect (PS-005·006·009·010·026), Corsy (기존) → CORS (PS-021), 자체 시그니처 → 세션ID·주석 (PS-004·018·009 일부). 전부 CLI 단발이거나 기존 도구라, JVM 상주(ZAP) 하나가 사라지고 스택이 subprocess 모델로 통일됩니다.

### Question 3 — fan-out 메커니즘 (WorkQueue 컴포넌트 존재 여부)
US-026(bounded 동시성 fan-out)과 US-025(CAS-resume)를 어떻게 뒷받침할까요?

A) **큐 추상화 인터페이스 + 자가호스트 구현 (AI 권장)** — `WorkQueue` 인터페이스 뒤에 Redis/Valkey(asynq류) 기본 구현. 내구성 있는 재개(Warm-Standby NFR-06)와 cloud-agnostic(SQS 아님, NFR-04)을 동시 충족. 테스트용 in-memory 구현 가능. 재개 내구성의 진실 원천은 `TaskStore`의 CAS(US-025), 큐는 분배 담당.

B) **외부 큐 직결** (asynq/Redis, 인터페이스 없이) — coolant와 동일, 그러나 seam 약화

C) **in-process worker pool만** — 큐 없음. 가장 단순하나 프로세스 재시작 시 내구 재개 불가 → Warm-Standby와 충돌

X) 기타

[Answer]: A

### Question 4 — Checker 인터페이스 형태 (핵심 seam, NFR-02)
3종 체커의 실행 방식이 다릅니다: Observation/Native는 **Fetcher가 취득한 공유 응답**을 소비(FR-02 단일요청재사용), Probe는 대상 host/URL에 **자체 I/O**. 인터페이스를 어떻게 잡을까요?

A) **단일 `Checker` 인터페이스 + ScanContext 입력 (AI 권장)** — `Check(ctx ScanContext) []Finding`. `ScanContext`가 target과 (있으면) 공유 응답을 함께 운반. Observation/Native는 공유 응답을 읽고, Probe는 target만 읽어 자체 실행. 한 seam으로 3종을 모두 수용 → NFR-02 최대화. 도구 추가 시 오케스트레이터 무변경.

B) **3개 형제 인터페이스** (`ObservationChecker`/`ProbeChecker`/`NativeChecker`) + 공통 마커 — 이질성을 타입으로 인정하나 registry/오케스트레이터가 3종을 알게 됨(seam 약화)

C) **단일 Checker + 별도 Fetcher, Probe는 Checker 밖** — Probe를 체커가 아닌 별도 컴포넌트 종류로 분리

X) 기타

[Answer]: A

### Question 5 — 매핑 계층의 형태 (FR-14 / NFR-15 / C-5)
CWE → {KISA PS-ID, OWASP, CAPEC} 매핑을 무엇으로 구현할까요?

A) **선언적 버전관리 파일** (YAML/JSON, repo에 커밋) — 시작 시 로드. **(AI 권장: FR-14 "매핑은 코드가 아니라 테이블", NFR-15 "프레임워크 추가 = 테이블 추가", C-5 "리뷰 가능한 1급 산출물"을 모두 직역. cloud-agnostic — 매핑에 DB 불필요. `no-canonical-cwe` 마커도 데이터로 표현)**

B) **Go 코드 (map 리터럴)** — 타입 안전하나 "프레임워크 추가 = 코드 변경"이라 NFR-15 취지와 충돌

C) **외부 config store/DB 테이블** — 런타임 갱신 가능하나 런타임 의존 추가

X) 기타

[Answer]: A

### Question 6 — 영속성 경계
Task 상태와 Findings를 어떻게 저장할까요?

A) **단일 store + repository 인터페이스 (AI 권장)** — 논리적 단일 store(기본 MongoDB), Task/Findings는 별도 컬렉션. **repository 인터페이스 뒤에 두어** 구현 교체 가능(NFR-04: Mongo/Postgres). coolant의 Mongo 패턴을 seam 뒤로.

B) **2개 store 분리** — Task 상태는 빠른 KV(Redis), Findings는 document store. 성능 분리되나 운영 복잡도↑

C) **단일 store 직결** (인터페이스 없이) — 단순하나 NFR-04 교체성 상실

X) 기타

[Answer]: A. 사용자의 분석 요청은 큰 작업일텐데, Task는 WorkQueue로 분배되는 단위랑 같아, 달라?

### Question 7 — API 소유·버저닝
스캔 요청/상태/리포트 API(US-001, US-023)를 어디에 둘까요? (외부 **비통제** 소비자 존재, Q3=A)

A) **Platform의 전용 `ScanAPI` 컴포넌트 (AI 권장)** — 단일 API 표면을 Platform이 소유, Scan/Findings 서비스로 위임. 계약을 한 곳에서 **버전관리**(P4 관심사). coolant httpapi 패턴(AWS-strip).

B) **컨텍스트별 분산 API** — Scan/Findings가 각자 엔드포인트 노출. 계약 일관성 약화

X) 기타

[Answer]: A

---

## D. 실행 체크리스트

### Part 1 — Planning
- [x] D-1. 컨텍스트 분석 (§A) + 후보 컴포넌트 인벤토리 (§B)
- [x] D-2. 설계 질문 작성 (§C, 7문항)
- [x] D-3. 사용자 답변 수집 — Q1/Q3/Q4/Q5/Q7=A 확정, Q2(ZAP 제거 재편)·Q6(되물음) 후속 필요
- [x] D-4. Step 8/9 분석 (3 rounds) → 전부 해소. Q2 재편 확정 (ZAP·Nuclei 제거, Observation native 10 + Probe CLI 12 + delegated 4)
- [x] D-5. 계획 승인 — requirements r3 + stories 재승인 완료 (2026-07-08)

### Part 2 — Artifact Generation (승인 후)
- [x] D-6. `components.md` — 컴포넌트명·목적·책임·인터페이스
- [x] D-7. `component-methods.md` — 메서드 시그니처·입출력 타입 (비즈니스 규칙은 Functional Design)
- [x] D-8. `services.md` — 서비스 정의·오케스트레이션 패턴 (ScanOrchestrator 중심)
- [x] D-9. `component-dependency.md` — 의존성 매트릭스·통신 패턴·데이터 플로우 (control→data 단방향 검증)
- [x] D-10. `application-design.md` — 위 4개 통합
- [x] D-11. 설계 완전성·일관성 검증 (모든 US가 ≥1 컴포넌트로 커버되는지 + 단방향 의존 위반 없는지)
- [x] D-12. 각 단계 완료 즉시 체크박스 갱신 + `aidlc-state.md` 갱신

---

## E. 확장 규칙 준수 (Planning 단계)
| Extension | 상태 |
|---|---|
| security-baseline | Q7(API authz SECURITY-08) · ApprovalGate/RateLimiter 컴포넌트(SECURITY-11) · FindingsStore 암호화(SECURITY-01)가 컴포넌트로 식별됨. 최소권한(06)·감사(14)는 Infra Design 이연. Blocking 없음. |
| resiliency-baseline | Q1(토폴로지)·Q3(내구 큐/CAS-resume)·ScanOrchestrator의 부분실패 격리(US-027)가 설계 대상. RTO/RPO 수치는 NFR Design. Blocking 없음. |
| property-based-testing | Normalizer/Deduplicator/ComplianceMapper/CoverageAccountant가 §7 property의 앵커 컴포넌트로 식별됨. 상세 property 표는 Functional Design. Blocking 없음. |
