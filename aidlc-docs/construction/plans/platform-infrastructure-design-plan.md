# Infrastructure Design Plan — unit: platform (U1)

**Stage**: CONSTRUCTION → Infrastructure Design (per-unit)
**Unit**: `platform` (U1, composition root)
**Created**: 2026-07-11
**Scope (사용자 선택)**: infra-plan.md §7 항목 1 — **WorkQueue → Redis/Valkey** + **TaskStore → Mongo**
(seam-swap: 인터페이스 불변, mem 구현 옆에 새 구현 추가, `cmd/chiller`에서만 선택)

**입력 아티팩트 (로드 완료)**:
- `construction/platform/functional-design/{domain-entities,business-rules,business-logic-model}.md` (E-5 seam, E-6 Infra-ready 규칙, PR-4 CAS 청구, PR-11 seam-swap)
- `construction/infrastructure-design/infra-plan.md` (§0 원칙, §1 표 #1·#2, §6 토폴로지, §7 순서)
- `construction/platform/code/business-logic-summary.md` (MVP 구현 범위 + honest scope)
- `aidlc-state.md` §Open design questions (미해결 3건이 이 단계로 귀속)
- 확장 규칙 (모두 Enabled, blocking): `security-baseline`, `resiliency-baseline`, `property-based-testing` (FULL)

---

## 0. 코드베이스 실사 결과 (질문 설계의 근거)

플랜을 세우기 전에 현재 코드를 확인했고, **infra-plan.md의 가정 중 하나가 사실과 다릅니다.**

| # | 실사 항목 | 확인된 사실 | 영향 |
|---|---|---|---|
| F-1 | seam 인터페이스 | `internal/platform/seams.go`에 `TaskStore`(11 메서드) · `WorkQueue`(3 메서드) 정의. 컴파일타임 assertion `var _ platform.TaskStore = (*Mem)(nil)` 존재 | seam은 그대로 사용 가능 (E-6 준수: ctx-first, error-last, CAS `(bool,error)`) |
| F-2 | **계약 테스트 부재** | `taskstore_test.go` / `workqueue_test.go`는 **구상 생성자 `New()`에 하드와이어된 개별 테스트 8개**. 생성자 테이블로 파라미터화된 계약 스위트가 **없음** | infra-plan §0 "기존 계약 테스트를 재사용" 가정이 **성립하지 않음**. 새 구현 추가 전에 **계약 스위트 추출 리팩터가 선행되어야 함** (Q10) |
| F-3 | **직렬화 태그 부재** | `Scan`·`WorkLedgerEntry`·`WorkItem`·`WorkItemOutcome`·`Target`·`ScanRequest`·`report.Report` 어디에도 `bson:`/`json:` 태그 없음. E-6 규칙 4의 "직렬화 가능"은 "함수/채널 없음"만 의미했음 | Mongo/Redis 인코딩 키가 **Go 필드명 그대로** 고정됨. 필드 리네임이 곧 스키마 파괴 → 태그 정책 결정 필요 (Q4) |
| F-4 | **enum이 int iota** | `ScanState`·`ItemState`가 `int` iota. `String()`은 있으나 파서 없음 | int로 저장하면 상수 순서 변경 시 기존 문서가 조용히 오염됨. CAS 필터도 int 비교 (Q4에 종속) |
| F-5 | composition root 하드 가드 | `cmd/chiller/main.go:75-77` — `f.store != "mem" \|\| f.queue != "mem"` 이면 에러 반환. seam 조립은 `orchestrator.Config`에 무조건 `taskstore.New()`/`workqueue.New()` 주입 | 가드를 `switch`로 교체하는 것이 유일한 배선 변경점 (PR-11b 준수) |
| F-6 | 의존성 | `go.mod`에 redis/valkey/mongo 클라이언트 **없음**. 직접 의존은 playwright-go, retryablehttp-go, yaml.v3뿐 | 신규 모듈 의존 추가 필요 → SECURITY-10(핀 고정·SBOM·취약점 스캔) 적용 대상 |
| F-7 | `Orchestrator` 배선 | composition root는 `platform.Orchestrator` 인터페이스가 아니라 구상 `*orchestrator.Orchestrator`를 사용 | 백엔드 교체와 무관 (Store/Queue만 주입 지점). 변경 불필요 |
| F-8 | 역할 분리 현황 | `--role=worker`는 mem 큐에서 no-op (문서화된 사실). `all`이 유일한 동작 경로 | Redis 큐 도입이 **manager/worker 실제 분리를 처음으로 활성화** → 이 단계에서 토폴로지 결정 필요 (Q1·Q2) |
| F-9 | 배포 자산 | `deploy/`는 `.keep`만 존재. Dockerfile·매니페스트·compose 파일 없음 | 배포 아티팩트를 이 단계에서 처음 만든다 (Q1) |

**F-2와 F-3은 이 단계의 선행 작업(pre-work)으로 플랜에 포함**되어야 합니다. 아래 Phase 0가 그것입니다.

---

## 1. 실행 플랜 (체크박스)

> 규율: 각 단계 완료 즉시 해당 체크박스를 `[x]`로 갱신한다 (Plan-Level Checkbox Enforcement).

### Phase 0 — 설계 선행 조건 확정 (이 문서에서 해결)
- [x] P0-1: 입력 아티팩트 로드 (functional-design, infra-plan, code summary, state)
- [x] P0-2: 코드베이스 실사 — seam 시그니처·테스트 구조·composition root·의존성 (§0 표)
- [x] P0-3: infra-plan 가정 검증 → F-2(계약 스위트 부재)·F-3(태그 부재)·F-4(int enum) 발견, 질문으로 승격
- [x] P0-4: 확장 규칙 적용 범위 사전 평가 (§3 표)
- [x] P0-5: 질문 생성 (§2, Q1–Q12) — **사용자 답변 대기 중**
- [x] P0-6: 답변 수집 + 모순/모호성 검사 → Q3=X(되물음)·Q7=B(Streams↔seam Ack 없음) 2건을 `platform-infrastructure-design-clarification-questions.md`로 승격 (C1·C2). 나머지 10개는 정합 확인 후 채택. **C1·C2 답변 대기 중**

### Phase 1 — 인프라 설계 아티팩트 생성 (답변 확정 후)
- [x] P1-1: `construction/platform/infrastructure-design/infrastructure-design.md` — 논리→물리 매핑·스키마·CAS·직렬화·공급망·관측·회복탄력성·데이터계약
- [x] P1-2: `construction/platform/infrastructure-design/deployment-architecture.md` — 토폴로지(Mermaid+텍스트), 배포단위, 장애격리, 배포/롤백, 미해결질문 해소
- [x] P1-3: `construction/shared-infrastructure.md` — Q9=C (Mongo 공유/컬렉션 분리, Valkey 큐·캐시 분리) + #5/#6 선반영
- [x] P1-4: 데이터 계약 명세 — infrastructure-design.md §2.2/§7 (컬렉션·인덱스·CAS 필터·스트림 키·마이그레이션)
- [x] P1-5: 미해결 설계 질문 3건 해소 기록 — deployment-architecture.md §5 + aidlc-state.md 갱신
- [x] P1-6: 확장 규칙 컴플라이언스 표 — `compliance-summary.md` (SECURITY/RESILIENCY/PBT, blocking 0)
- [x] P1-7: 콘텐츠 검증 — Mermaid 노드 ID 영숫자·라벨 특수문자 없음(검증), ASCII 대신 Mermaid+텍스트 대안 채택
- [ ] P1-8: 완료 메시지 제시 + 명시적 승인 대기 ← **현재 여기**

### Phase 2 — Code Generation으로 이월될 항목 (여기서는 설계만; 실제 코드는 다음 스테이지)
- [ ] P2-1: 계약 스위트 추출 리팩터 (F-2) — `taskstore/contract_test.go`, `workqueue/contract_test.go`
- [ ] P2-2: `taskstore_mongo.go` · `workqueue_redis.go` 구현
- [ ] P2-3: `cmd/chiller` 하드 가드 → `switch` 교체 (F-5)
- [ ] P2-4: 직렬화 태그/enum 정책 적용 (F-3·F-4)
- [ ] P2-5: 통합 테스트 하네스 (Q10에서 선택한 방식)
- [ ] P2-6: 배포 자산 (Dockerfile, 매니페스트/compose) — Q1·Q11 종속

---

## 2. 질문 (Infrastructure Design)

**답변 방법**: 각 질문의 `[Answer]:` 뒤에 선택지 문자를 적어 주세요. 해당하는 선택지가 없으면 마지막 `X) Other`를 고르고 설명을 덧붙여 주세요. 다 쓰신 뒤 "완료"라고 알려 주시면 읽고 진행합니다.

---

### 카테고리 A — Deployment Environment

## Question 1
chiller의 **배포 대상 환경**은 무엇입니까? (요구사항 라운드2 Q4=B "AWS 의존 제거 / 클라우드 비종속"이 이미 확정되어 있습니다.)

A) **Kubernetes** (온프레미스 또는 어떤 클라우드든 무관) — manager/worker를 Deployment로, Redis·Mongo는 클러스터 내 StatefulSet 또는 외부 관리형 엔드포인트를 주입. coolant의 K8s JobRunner 패턴을 참조 가능.

B) **단일 호스트 docker-compose** — chiller + Redis + Mongo를 한 compose 파일로. 개발·소규모 운영용. 수평 확장은 replica 수동 조정.

C) **둘 다 산출** — compose는 로컬 개발·통합 테스트용, K8s 매니페스트는 운영용. 동일 이미지, 다른 배선.

D) **바이너리 + systemd** (컨테이너 없음) — Redis·Mongo는 기존 사내 인프라의 엔드포인트를 주입받음.

X) Other (please describe after [Answer]: tag below)

[Answer]: C

---

### 카테고리 B — Compute Infrastructure

## Question 2
Redis 큐가 들어오면 `--role=worker`가 처음으로 실제 동작합니다 (F-8). **worker의 컴퓨트/스케일링 모델**을 무엇으로 합니까?

A) **상주 worker 풀** — 고정 replica 수의 장기 실행 프로세스가 Redis에서 계속 pull. 스캔이 없으면 유휴 블로킹. 가장 단순하고 MVP 코드(`--workers=N` 내부 고루틴 풀)와 그대로 맞음.

B) **큐 깊이 기반 오토스케일** — 상주 풀 + 큐 길이 메트릭으로 replica 자동 증감 (K8s HPA/KEDA 류). RESILIENCY-09(오토스케일링) 충족. 큐 깊이 메트릭 노출 코드가 추가로 필요.

C) **스캔당 Job** — 스캔 시작 시 worker Job을 N개 띄우고 스캔 종료 시 소멸 (coolant JobRunner 패턴). 자원 격리는 최상, 기동 지연·복잡도 상승.

X) Other (please describe after [Answer]: tag below)

[Answer]: ~~C~~ → **B** (2026-07-11 사용자 정정: proxy 싱글턴화 후 worker를 스캔에 묶을 이유가 사라졌고, Streams consumer group이 상주 풀 모델과 정합. 큐 깊이 기반 오토스케일 상주 풀 채택. 큐도 단일 공유 스트림으로 통일, Drain=drained-set+lazy XACK-skip)

---

## Question 3
manager는 API + FSM + 조립(fold/report)을 담당합니다. **manager의 가용성 형태**는?

A) **단일 인스턴스** — API가 단일 진입점. 장애 시 재기동으로 복구(작업은 Mongo 원장 + Redis 큐에 남아 있으므로 resume 가능). 가장 단순.

B) **다중 인스턴스 + 로드밸런서** — API는 무상태이므로 수평 확장. 단, `StartScan`/`ResumeScan`의 fan-out과 최종 fold를 **정확히 한 인스턴스만** 수행하도록 리더 선출 또는 CAS 기반 단일 실행 보장이 필요 (설계 부담 증가).

C) **다중 인스턴스, 단 fan-out/fold는 Mongo CAS로 단일 승자 보장** — 리더 선출 없이 기존 `CompareAndSetState`(created→running 전이)를 그대로 단일 승자 게이트로 사용. 이미 있는 CAS를 재사용하므로 추가 인프라 없음.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

---

### 카테고리 C — Storage Infrastructure (TaskStore → Mongo)

## Question 4
F-3·F-4 발견 사항: 영속 엔티티에 `bson` 태그가 없고, `ScanState`/`ItemState`가 `int` iota입니다. Mongo에 저장할 때 **문서 스키마를 어떻게 확정**합니까?

A) **도메인 타입에 `bson` 태그 추가 + enum은 문자열로 저장** — `ScanState`/`ItemState`에 `MarshalBSONValue`/`UnmarshalBSONValue`(또는 문자열 파서)를 붙여 `"running"`처럼 저장. 필드 리네임·상수 재정렬에 안전. 도메인 패키지에 인프라 관심사(태그)가 침투하는 비용.

B) **인프라 전용 DTO 매핑 레이어** — `taskstore_mongo.go` 안에 `scanDoc`/`ledgerDoc` 구조체를 두고 도메인 ↔ DTO 변환 함수 작성. 도메인은 인프라를 전혀 모름(헥사고날 원칙 유지). 코드량 증가 + 왕복 변환 테스트 필요(PBT-02 round-trip 대상).

C) **태그 없이 기본 인코딩 사용** — Go 필드명이 곧 BSON 키. 가장 코드가 적지만 필드 리네임이 조용한 스키마 파괴를 일으키고 enum은 int로 저장됨. (F-3·F-4 위험을 그대로 수용)

X) Other (please describe after [Answer]: tag below)

[Answer]: A

---

## Question 5
`TaskStore`의 CAS 두 메서드(`CompareAndSetState`, `CompareAndSetItemState`)는 Mongo에서 **원자성**을 보장해야 합니다 (PR-4 단일 승자 청구 — 이게 깨지면 한 WorkItem을 두 worker가 실행). 구현 방식은?

A) **`findOneAndUpdate` + expected-state 필터** — 단일 문서 원자 조건부 갱신. 트랜잭션 불필요, 단일 노드 Mongo에서도 동작. 원장 엔트리를 **독립 문서**로 저장해야 함(`scan` 컬렉션 / `workledger` 컬렉션 분리).

B) **Scan 문서 안에 원장 배열 임베드 + 배열 필터 조건부 갱신** — 문서 1개로 스캔 전체를 표현. 16MB 문서 한계와 대형 스캔의 배열 경합이 걸림돌.

C) **멀티도큐먼트 트랜잭션 사용** — replica set 필수. 가장 강한 보장이지만 배포 요구(replica set)와 지연이 늘어남.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

---

## Question 6
Mongo의 **운영 형태와 내구성**은? (RESILIENCY-08 멀티AZ 베이스라인 · RESILIENCY-12 백업 · SECURITY-01 전송/저장 암호화가 여기에 걸립니다.)

A) **단일 노드 Mongo** — 개발/PoC용. 멀티AZ·복제 없음. RESILIENCY-08/12에 대해 **명시적 예외 문서화**가 필요(운영 배포 아님을 못박음).

B) **3-노드 replica set (자체 운영)** — 멀티AZ 분산, 자동 장애조치, 트랜잭션 가능. 백업은 스케줄 `mongodump` 또는 스냅샷. 클라우드 비종속 유지.

C) **관리형 Mongo 서비스 엔드포인트 주입** — 복제·백업·암호화를 서비스에 위임. 접속 문자열만 시크릿으로 주입. 벤더 종속은 "엔드포인트 주입" 수준으로 제한.

X) Other (please describe after [Answer]: tag below)

[Answer]: B

---

### 카테고리 D — Messaging Infrastructure (WorkQueue → Redis/Valkey)

## Question 7
큐 백엔드와 **배달 방식**은? (`Dequeue`는 ctx로 블록해야 하고, at-least-once면 충분합니다 — 정확히-한-번 효과는 원장 CAS 청구(PR-4)가 흡수한다고 이미 설계돼 있습니다.)

A) **Redis LIST + `BLPOP`** — infra-plan §1 #2의 원안. 구현이 가장 단순(`RPUSH`/`BLPOP`/scan별 키 삭제=Drain). 단점: worker가 pop한 뒤 죽으면 그 item은 큐에서 사라짐 → **원장의 `inflight` 항목을 재개 시 재적재**하는 경로가 반드시 있어야 함(`ResumeScan` + `ListUnfinished`가 이미 그 역할).

B) **Redis Streams + consumer group** — `XREADGROUP`/`XACK`/`XAUTOCLAIM`. pop 후 크래시해도 pending 엔트리를 다른 worker가 자동 회수(가시성 타임아웃). 재개 경로에 덜 의존. 구현·운영 복잡도 상승.

C) **asynq 라이브러리 채택** — coolant가 쓰던 `TaskManager` 패턴을 그대로. 재시도·가시성·대시보드를 얻지만, 라이브러리의 태스크 모델을 우리 `WorkQueue` 3-메서드 seam 뒤에 억지로 맞춰야 함.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

---

## Question 8
Redis 계열 **엔진 선택**은? (라이선스·에코시스템 문제)

A) **Valkey** — Redis 7.2의 BSD 포크(Linux Foundation). 라이선스 리스크 없음. `go-redis` 클라이언트 그대로 호환.

B) **Redis (RSAL/SSPL 라이선스 버전)** — 사내에 이미 Redis 운영 표준이 있는 경우.

C) **접속 문자열만 주입, 엔진 불문** — 코드는 RESP 프로토콜만 알고 배포 시점에 결정. 설계 문서에는 "Redis-호환"으로 기재.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

---

### 카테고리 E — Shared Infrastructure

## Question 9
infra-plan에는 **같은 종류의 백엔드를 쓰는 다른 seam**이 대기 중입니다: `ResponseCache`→Valkey(#5), `FindingsStore`→Mongo(#6). 이번 설계에서 인스턴스를 어떻게 다룹니까?

A) **인스턴스 공유, 논리 분리** — Redis 한 인스턴스를 큐와 캐시가 공유(키 접두사 또는 DB 인덱스 분리), Mongo 한 인스턴스를 taskstore와 findings가 공유(컬렉션 분리). 운영 단순. 캐시 부하가 큐를 방해할 위험은 문서화.

B) **전용 인스턴스** — 큐용 Redis / 캐시용 Redis, 오케스트레이션용 Mongo / findings용 Mongo를 분리. 격리(bulkhead, RESILIENCY-10) 최상, 운영 비용 증가.

C) **혼합** — Mongo는 공유(컬렉션 분리), Redis는 분리(큐는 내구성·캐시는 휘발성이라 메모리 정책·eviction 정책이 정반대이므로).

X) Other (please describe after [Answer]: tag below)

[Answer]: C

---

### 카테고리 F — 테스트 전략 (F-2 해소 + PBT 확장 규칙)

## Question 10
F-2: 생성자 파라미터화된 계약 스위트가 **아직 없습니다**. 새 백엔드를 검증할 방법은? (PBT 확장이 FULL blocking이라 상태 기반 속성 테스트(PBT-06)가 seam에 적용 대상입니다.)

A) **계약 스위트 추출 + 실제 백엔드 컨테이너로 실행** — 기존 8개 테스트를 `func testTaskStore(t, newStore func() platform.TaskStore)` 형태로 리팩터하고, mem과 Mongo/Redis 양쪽에 대해 실행. 컨테이너는 testcontainers-go로 테스트 안에서 기동. CI에 Docker 필요. 가장 충실한 검증.

B) **계약 스위트 추출 + 인메모리 fake 서버로 실행** — `miniredis` 같은 프로세스 내 서버로 Redis를 대체, Mongo는 별도 fake가 마땅치 않아 통합 테스트를 build tag(`//go:build integration`)로 분리하고 기본 CI에서는 제외. 빠르지만 실제 Mongo 의미론(CAS 필터)을 검증하지 못함.

C) **계약 스위트 추출 + 양쪽 병행** — 단위 테스트는 mem + miniredis로 항상 실행, 실제 Mongo/Redis 컨테이너 대상 통합 스위트는 build tag 뒤에 두고 CI의 별도 잡에서 실행. PBT-06 상태 모델 테스트는 mem을 오라클(PBT-05)로 삼아 백엔드 구현과 명령 시퀀스 결과를 비교.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

---

### 카테고리 G — 미해결 설계 질문 해소 (aidlc-state.md §Open design questions)

## Question 11
요구사항 라운드2에서 **서로 충돌하는 답변**이 있었고 "NFR/Infra Design에서 해소"로 이월되었습니다. 지금 확정해 주세요.

- 충돌 1: Q2r2 = **C (Warm Standby, RPO/RTO 분 단위, 크로스리전 함의)** ↔ Q7r2 = **X (멀티리전 불필요)**
- 충돌 2: Q5r2 = **C (canary 자동 롤백)** ↔ Q6r2 = **C (blue/green 배포)**

A) **단일 리전 · 멀티 AZ + blue/green + 버전 고정 롤백** — Warm Standby를 "리전 내 다중 AZ 상시 가동"으로 재해석(크로스리전 폐기, Q7r2=X 존중). 배포는 blue/green, 롤백은 이전 환경으로 스왑. canary는 폐기. (RESILIENCY-08=A, RESILIENCY-04 롤백=B, 배포=C)

B) **단일 리전 · 멀티 AZ + canary + 지표 기반 자동 롤백** — 배포는 canary(점진 트래픽 전환), 롤백은 헬스/지표 회귀 시 자동. blue/green은 폐기. chiller는 사용자 트래픽을 받는 서비스가 아니라 스캔 워커라, "트래픽 전환" 대신 "worker replica 일부만 신 버전"으로 canary를 해석. (RESILIENCY-08=A, RESILIENCY-04 롤백=C, 배포=D)

C) **단일 리전 · 멀티 AZ + rolling 배포 + 버전 고정 롤백** — 가장 단순. chiller는 내부 배치성 스캐너이므로 무중단 컷오버가 불필요하다고 판단. canary·blue/green 둘 다 폐기하고 요구사항을 정정. (RESILIENCY-08=A, RESILIENCY-04 롤백=A, 배포=B)

D) **멀티리전 active-passive 유지** — Warm Standby를 문자 그대로 크로스리전으로 구현하고 Q7r2=X를 정정. 비용·복잡도 최대.

X) Other (please describe after [Answer]: tag below)

[Answer]: C

---

## Question 12
RESILIENCY-02는 **RTO/RPO 수치**를, A-4(미해결)는 **처리량/지속시간 수치 경계**를 요구합니다. chiller의 데이터 손실 허용 범위는?

A) **RPO = 진행 중 스캔 손실 허용 (스캔 재실행으로 복구)** · **RTO = 수 분** — 스캔은 재현 가능한 읽기 전용 관측이므로, 최악의 경우 스캔을 처음부터 다시 돌리면 됨. Mongo는 원장/보고서 내구성만 책임. 백업은 일 1회 스냅샷 + 7일 보관.

B) **RPO ≈ 0 (완료된 WorkItem 결과는 절대 유실 불가)** · **RTO = 수 분** — 원장 write를 majority write-concern으로. 진행 중 스캔은 `ResumeScan`으로 이어받아야 하며 재실행은 허용하지 않음. 백업은 지속 oplog + 시점 복구.

C) **RPO = 완료 보고서만 내구 (WorkItem 단위 진행은 유실 허용)** · **RTO = 수십 분** — 보고서 저장 이후만 보호. 진행 중이던 스캔은 재실행. 백업은 일 1회.

X) Other (please describe after [Answer]: tag below)

[Answer]: B

---

## 3. 확장 규칙 적용 범위 (사전 평가 — P1-6에서 최종 판정)

이 스테이지에서 생성하는 것은 **설계 문서**이므로, 코드 수준 규칙은 대부분 다음 Code Generation 스테이지로 이월됩니다. 다만 설계 문서가 근거를 제공해야 하는 규칙은 아래와 같습니다.

| 규칙 | 이 스테이지 관련성 | 근거 |
|---|---|---|
| SECURITY-01 (전송/저장 암호화) | **적용** | Mongo/Redis 접속 TLS 강제, 저장 암호화 — Q6 종속 |
| SECURITY-03 (구조적 로깅) | 적용 | 백엔드 호출 실패 로깅 경로 설계 |
| SECURITY-06 (최소 권한) | **적용** | Mongo 사용자 권한을 컬렉션 단위로, Redis ACL을 키 접두사 단위로 |
| SECURITY-07 (네트워크 제한) | **적용** | Redis/Mongo를 내부 네트워크로만 노출 — Q1 종속 |
| SECURITY-09 (기본 자격증명 금지) | **적용** | Redis `requirepass`/Mongo 인증 필수, 기본 계정 비활성 |
| SECURITY-10 (공급망) | **적용** | 신규 클라이언트 의존(go-redis, mongo-driver) 버전 핀 + 취약점 스캔 + 이미지 태그 핀 (F-6) |
| SECURITY-12 (자격증명 관리) | **적용** | 접속 문자열은 시크릿 마운트/환경변수, 소스 하드코딩 금지 |
| SECURITY-15 (fail-closed) | **적용** | 백엔드 연결 실패 시 스캔 시작 거부(부팅 게이트 PR-9와 정합) |
| SECURITY-02/04/05/08/11/13/14 | 대체로 이월 또는 N/A | API 표면·헤더·입력검증은 기존 `api` 패키지 소관, 이번 변경 없음 |
| RESILIENCY-01 (워크로드 분류) | **적용** | manager/worker/proxy + Mongo/Redis 임계도 분류 + 의존 맵 |
| RESILIENCY-02 (SLA/RTO/RPO) | **적용** | Q12 |
| RESILIENCY-03/04 (변경관리·배포/롤백) | **적용** | Q11 (Q3r2=B, Q4r2=B에 따라 AI-DLC가 제안) |
| RESILIENCY-05/06/07 (관측·헬스체크) | **적용** | 백엔드 연결 deep health check 필요. OTel exporter 자체는 seam #3(별도 항목)이나 헬스 엔드포인트는 여기서 설계 |
| RESILIENCY-08 (멀티AZ/리전) | **적용** | Q11 |
| RESILIENCY-09 (오토스케일) | **적용** | Q2 |
| RESILIENCY-10 (타임아웃·서킷·벌크헤드) | **적용** | Mongo/Redis 호출 타임아웃, 연결 풀 한계, Q9(격리) |
| RESILIENCY-11/12/13 (DR·백업·페일오버) | **적용** | Q6·Q12 |
| RESILIENCY-14/15 (테스트·IR) | **적용** | Q3r2/Q8r2=B(제안) → 시나리오를 문서화하고 Operations로 이월 |
| PBT-01 (속성 식별) | **적용** | seam 계약의 속성(단일 승자 CAS, FIFO, 멱등 Drain, 직렬화 왕복)을 설계 문서에 기재 |
| PBT-02 (왕복) | **적용(설계)** | Q4=B 선택 시 도메인↔DTO 왕복이 PBT-02 대상 |
| PBT-05 (오라클) | **적용(설계)** | mem 구현이 신규 백엔드의 오라클 — Q10=C |
| PBT-06 (상태 기반) | **적용(설계)** | TaskStore/WorkQueue 모두 상태 보유 → 명령 시퀀스 PBT 대상 |
| PBT-03/04/07/08/09/10 | 코드 생성으로 이월 | 구현 시 판정 |

---

## 4. 다음 단계

1. 위 Q1–Q12에 답변 → "완료" 통보
2. AI가 답변의 모순/모호성 검사 (필요 시 `platform-infrastructure-design-clarification-questions.md` 생성)
3. Phase 1 아티팩트 생성 → 완료 메시지 + 승인 대기
4. 승인 후 Code Generation 스테이지 (Phase 2 항목 구현)
