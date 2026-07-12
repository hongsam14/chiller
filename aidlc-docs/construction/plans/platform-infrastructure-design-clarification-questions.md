# Infrastructure Design — Clarification Questions (platform / U1)

답변을 읽고 2가지를 확인해야 합니다. Q1·Q2·Q4·Q5·Q6·Q8·Q9·Q10·Q11·Q12는 명확하고 서로 정합적이라 그대로 채택합니다. 아래 2건만 답해 주세요.

**답변 방법**: 각 `[Answer]:` 뒤에 문자를 적고, 없으면 `X) Other`로 설명. 다 쓰시면 "완료"라고 알려 주세요.

---

## Clarification 1 — Q3: manager와 proxy는 다른 층위입니다 (되물으신 질문에 대한 답 + 확정)

**되물으신 것**: "스캔 요청 타겟마다 프록시 브라우저를 띄워야 하기 때문에 각각 매니저가 있어야 함. 아닌가?"

**핵심 구분** — 두 가지는 별개입니다:

- **proxy (데이터 플레인, 스캔당)**: 맞습니다. 금융 모드에서 프록시는 **단일 인증 세션 불변식**을 가집니다 (storageState 1개 = 인증된 브라우저 컨텍스트 1개, 탭풀 T). 서로 다른 스캔 타겟은 서로 다른 storageState라 **한 proxy를 공유할 수 없습니다** → **proxy는 스캔당 1개**가 맞습니다. (비금융 스캔은 proxy가 아예 필요 없음)
- **manager (컨트롤 플레인)**: API + FSM + fold/report **조립**만 담당합니다. 이건 브라우저 세션과 무관한 오케스트레이션 관심사라, **한 manager가 여러 스캔을 오케스트레이션할 수 있습니다.** manager를 스캔당 띄울 필요는 없습니다.

즉 되물으신 직관은 **proxy에 대해서는 정확**하지만, 그 결론이 manager까지 확장되지는 않습니다. 다만 Q2=C(스캔당 worker Job)를 고르셨으므로, "스캔당 자원 묶음"을 어떻게 구성할지 두 가지 형태가 가능합니다. 어느 쪽인지 확정해 주세요.

### Clarification Question 1
스캔 1건의 실행 단위(worker Job들 + 금융 시 proxy 1개)를 **누가 띄우고 추적**합니까?

A) **상주 컨트롤 플레인 + 스캔당 데이터 플레인 (권장)** — 장기 실행 manager 1계층이 API·FSM·fold를 소유하고, 스캔이 시작되면 그 스캔의 worker Job N개 + (금융 시) proxy Job 1개를 띄운다. 스캔 종료 시 worker/proxy Job은 소멸하지만 manager는 계속 산다. 그 스캔의 worker들은 그 스캔의 proxy를 가리킨다. → **manager 상주 · proxy 스캔당 · worker 스캔당.** 기존 코드(`orchestrator`가 컨트롤 플레인, proxy가 세션당)와 그대로 맞음. manager 다중화는 기존 `CompareAndSetState`(created→running) CAS로 단일 승자 보장(원래 Q3의 C안).

B) **스캔당 코디네이터 (상주 manager 없음)** — 얇은 API 프런트가 요청만 받아 **스캔 스코프 코디네이터 Job**을 하나 띄우고, 그 코디네이터가 자기 스캔의 FSM + worker Job들 + proxy를 소유하다가 스캔이 끝나면 자신도 소멸한다. 되물으신 "각각 매니저" 직관을 문자 그대로 구현. 스캔 간 완전 격리(bulkhead)가 최대지만, FSM/fold가 코디네이터로 이동하고 Mongo/Redis 접속을 코디네이터마다 새로 열어야 함.

X) Other (please describe after [Answer]: tag below)

[Answer]:

---

## Clarification 2 — Q7=B: Streams는 Ack가 필요한데, seam은 Ack 메서드가 없습니다

**충돌 내용**: Q7에서 **Redis Streams + consumer group (B)**를 고르셨습니다. Streams의 가치는 `XREADGROUP`으로 받은 메시지를 **처리 완료 후 `XACK`** 하고, 죽은 컨슈머의 미확인(pending) 엔트리를 `XAUTOCLAIM`으로 회수하는 데 있습니다. 그런데 `WorkQueue` seam은 **`Enqueue`/`Dequeue`/`Drain` 3개뿐이고 Ack 메서드가 없습니다.** 사용자가 강조하신 **PR-11 "seam 인터페이스 불변"** 원칙과, infra-plan §0의 "Infra 구현이 인터페이스 변경을 요구하면 그건 설계 냄새"라는 경고에 정면으로 걸립니다.

세 가지 해소 방법이 있고, 어느 쪽인지 정해 주세요.

### Clarification Question 2
Streams의 Ack/회수를 3-메서드 seam 안에서 어떻게 처리합니까?

A) **내부 reaper로 흡수, 인터페이스 불변 (권장)** — seam은 그대로 3개. `Dequeue`=`XREADGROUP`. 배경 고루틴(reaper)이 주기적으로 `XAUTOCLAIM`으로 오래된 pending 엔트리를 회수해 재배달하고, **원장 CAS(PR-4)로 이미 claim/완료된 item은 `XACK`만 하고 버린다**(재배달돼도 CAS가 중복을 거른다 = at-least-once + visibility를 CAS로 흡수, E-6 규칙 5 그대로). Ack 신호가 seam 밖(reaper 내부 + 원장 상태)에 있으므로 **인터페이스 변경 없음.** Q7=B의 취지("재개 경로에 덜 의존")도 유지 — 크래시 회수를 `ResumeScan`이 아니라 `XAUTOCLAIM`이 처리.

B) **BLPOP으로 되돌림 (infra-plan 원안, Q7의 A안)** — Streams를 버리고 단순 LIST+`BLPOP`. 크래시 회수는 `ResumeScan`+`ListUnfinished`(원장의 inflight 재적재)에 의존. 가장 단순하고 Ack 개념 자체가 없음. 대신 Q7=B에서 얻으려던 "재개 경로 비의존" 이점을 포기.

C) **seam에 Ack 메서드 추가** — `WorkQueue`에 `Ack(ctx, item) error`를 넣어 Streams를 정석대로 사용. 가장 깔끔한 Streams 사용이지만 **PR-11 "인터페이스 불변"을 깨고** mem 구현·계약 테스트·모든 호출부를 함께 고쳐야 함(사용자 강조 원칙 위반 — 명시적으로 표시).

X) Other (please describe after [Answer]: tag below)

[Answer]:

---

## 확정된 답변 요약 (참고 — 재확인 불필요)

| Q | 답 | 채택 내용 |
|---|---|---|
| Q1 | C | compose(로컬/통합테스트) + K8s(운영), 동일 이미지·다른 배선 |
| Q2 | C | 스캔당 worker Job (coolant JobRunner 패턴), 스캔 종료 시 소멸 |
| Q3 | → C1 | (위 Clarification 1에서 확정) |
| Q4 | A | 도메인 타입에 `bson` 태그 + enum은 문자열 저장 (`ScanState`/`ItemState` Marshal/Unmarshal) |
| Q5 | A | `findOneAndUpdate` + expected-state 필터, 원장 독립 문서(`scan`/`workledger` 컬렉션 분리) |
| Q6 | B | 3-노드 replica set 자체 운영, `mongodump`/스냅샷 백업, 클라우드 비종속 |
| Q7 | B → C2 | Redis Streams + consumer group (Ack 처리 방식은 위 Clarification 2에서 확정) |
| Q8 | A | Valkey (BSD, `go-redis` 호환) |
| Q9 | C | Mongo 공유(컬렉션 분리) · Redis 큐/캐시 분리(내구성 vs 휘발성) |
| Q10 | A | 계약 스위트 추출 + testcontainers-go 실 컨테이너, CI에 Docker. (mem은 생성자 테이블에 남아 PBT-05 오라클·PBT-06 상태 모델 유지) |
| Q11 | C | 단일 리전·멀티 AZ + rolling 배포 + 버전 고정 롤백. canary·blue/green 폐기(요구사항 정정) |
| Q12 | B | RPO≈0(완료 WorkItem 결과 무손실, majority write-concern), RTO 수 분, 진행 스캔은 `ResumeScan`으로 이어받고 재실행 불가, 지속 oplog + 시점 복구 |

---

## 정정 (2026-07-11, 사용자 피드백 반영)

위 Clarification 1의 본문·선택지에 적힌 "**proxy는 스캔당 1개**"는 **틀렸습니다.** 사용자 지적: proxy의 존재 이유가 "**단일** 인증 세션"인데, proxy를 스캔 수만큼 늘리면 그 논리와 모순됩니다.

**정정된 결론**:
- **proxy = 상주 싱글턴** (`--role=proxy` replica 1). 하나의 storageState = 하나의 인증 세션. worker만 수평 확장하여 이 한 proxy를 공유(원설계 domain-entities.md §E-5, MVP `--storage-state` 단일 파일과 일치).
- **C1=A는 유지되나 재해석**: "상주 컨트롤 플레인 + 스캔당 데이터 플레인"에서 **데이터 플레인의 스캔당 부분은 worker만**. proxy는 manager와 함께 **상주 싱글턴 계층**.
- 귀결: chiller 한 배포는 한 번에 하나의 인증 세션만 보유 → 서로 다른 인증 대상 금융 스캔은 직렬화 또는 별도 배포. 비금융 스캔은 무관.
- 반영 위치: `infrastructure-design.md` §1, `deployment-architecture.md` §1(토폴로지·Mermaid)/§2/§3/§4.
