# Infrastructure Design — unit: platform (U1)

**Stage**: CONSTRUCTION → Infrastructure Design
**Scope**: seam-swap of `WorkQueue` → Valkey (Redis Streams) and `TaskStore` → Mongo, per infra-plan.md §1 #1–#2 / §7 item 1.
**Principle (E-6 / PR-11)**: 새 impl은 mem impl **옆에** 같은 인터페이스로 추가. `Orchestrator`·워커 루프·도메인 코드 무변경. 선택은 `cmd/chiller`(composition root)에서만.
**Locked decisions**: Q1=C · Q2=**B**(2026-07-11 정정: 큐 깊이 오토스케일 상주 worker 풀) · Q3=A · Q4=A · Q5=A · Q6=B · Q7=B · Q8=A · Q9=C · Q10=A · Q11=C · Q12=B · C1=A · C2=A (see `plans/platform-infrastructure-design-*.md`).

---

## 1. 논리 컴포넌트 → 실제 서비스 매핑

| 논리 컴포넌트 (seam / 역할) | MVP 구현 (유지) | 이번에 추가하는 Infra 구현 | 실제 서비스 |
|---|---|---|---|
| `platform.TaskStore` | `taskstore.Mem` | `taskstore.Mongo` (`taskstore_mongo.go`) | **MongoDB 3-노드 replica set** (자체 운영, Q6=B) |
| `platform.WorkQueue` | `workqueue.Mem` | `workqueue.Valkey` (`workqueue_valkey.go`) | **Valkey** (Redis Streams + consumer group, Q7=B/Q8=A) |
| `platform.ProgressReporter` | `progress.Mem` | (이번 스코프 밖 — infra-plan #3) | 인-메모리 유지 |
| `platform.Authorizer` | `api.BearerAuthorizer` | (이번 스코프 밖 — infra-plan #4) | 정적 토큰 유지 |
| manager 역할 (컨트롤 플레인: API+FSM+fold) | `orchestrator` + `api` | 배선만 변경 (Store/Queue 주입 교체) | **단일 상주 Deployment** (Q3=A, replica=1) |
| worker 역할 (체커 실행) | 내부 고루틴 풀 | **상주 오토스케일 풀** (Q2=B, 2026-07-11 정정) | **worker Deployment + HPA/KEDA**(큐 깊이 트리거), consumer group `workers`로 공유 스트림 경쟁 소비 |
| proxy 역할 (금융 단일 세션) | `proxy` 패키지 | 무변경 | **상주 싱글턴** (금융 세션 1개; 스캔당 아님 — 단일 세션 불변식). worker만 수평 확장. (2026-07-11 정정) |

**핵심**: 이번 스테이지가 실제로 바꾸는 것은 (a) 두 seam의 새 impl 파일 2개, (b) `cmd/chiller`의 하드 가드 → `switch`, (c) 도메인 enum에 BSON marshaling 추가(Q4=A). `Orchestrator`/워커 루프/`api`/`proxy`는 인터페이스에만 의존하므로 **코드 무변경**(PR-11a).

---

## 2. Storage — MongoDB (TaskStore)

### 2.1 배포 형태 (Q6=B, RESILIENCY-08/12, SECURITY-01)
- **3-노드 replica set**, 3개 AZ에 1노드씩 분산 (멀티AZ 정적 안정성). Primary 장애 시 자동 election.
- **자체 운영** → 클라우드 비종속 유지 (요구사항 라운드2 Q4=B). K8s에서는 StatefulSet 3 replica + headless Service, 온프레미스에서는 3 VM.
- **저장 암호화** (SECURITY-01): WiredTiger encryption-at-rest (keyfile 또는 KMIP). **전송 암호화**: `tls=true` 필수, 평문 접속 거부.
- **인증** (SECURITY-09/12): SCRAM 사용자, 기본 계정 없음. 접속 문자열은 K8s Secret/env로 주입, 소스 하드코딩 금지.
- **최소 권한** (SECURITY-06): chiller 서비스 계정은 `chiller` DB의 지정 컬렉션에만 readWrite. admin 권한 없음.

### 2.2 컬렉션 스키마 (Q5=A: 원장 독립 문서)

두 컬렉션으로 분리 (Q5=A — `findOneAndUpdate` 단일 문서 CAS를 쓰려면 원장 엔트리가 독립 문서여야 함):

| 컬렉션 | 문서 = | `_id` | 주요 필드 | 인덱스 |
|---|---|---|---|---|
| `scans` | `platform.Scan` 1건 | `ScanID` (string) | `state`(문자열), `request`, `frameworks`, `createdAt`, `updatedAt`, `report`(nullable) | `_id`; `{state:1}` (ListUnfinished 스캔 필터) |
| `workledger` | `platform.WorkLedgerEntry` 1건 | `WorkItemID` (string) | `scanId`, `item`(WorkItem 임베드), `state`(문자열), `outcome`(nullable) | `_id`; `{scanId:1, state:1}` (ListUnfinished/AllItems/Drain) |
| `reports` | `report.Report` 1건 | `ScanID` | 보고서 본문 | `_id` |

> `reports`를 `scans.report`에 임베드하지 않고 분리한 이유: 보고서가 커질 수 있고(증거 스니펫), `SaveReport`/`LoadReport`가 스캔 문서와 독립적으로 접근되기 때문. 단, `scans.report`는 nil 포인터로 두고 실제 본문은 `reports`에 저장(Q5=A와 정합).

### 2.3 직렬화 정책 (Q4=A, F-3/F-4 해소)
- 도메인/플랫폼 영속 타입에 `bson:"..."` 태그 추가: `Scan`, `WorkLedgerEntry`, `WorkItem`, `WorkItemOutcome`, `Target`, `ScanRequest`, `ScanOptions`.
- **enum은 문자열 저장**: `ScanState`·`ItemState`에 `MarshalBSONValue`/`UnmarshalBSONValue` 구현 → `"created"|"running"|"completed"|"stopped"|"failed"`, `"pending"|"inflight"|"done"`. `String()`은 이미 존재하므로 역파서(`parseScanState`)만 추가.
- **근거**: int iota 저장은 상수 재정렬 시 기존 문서를 조용히 오염시킴(F-4). 문자열이면 필드 리네임·상수 추가에 안전. CAS 필터도 문자열 비교가 되어 사람이 읽을 수 있음.
- **왕복 속성**(PBT-02): `Unmarshal(Marshal(x)) == x` 를 모든 영속 타입에 대해 property test로 검증(생성기는 도메인 제약 존중, PBT-07).

### 2.4 CAS 원자성 (Q5=A, PR-4 단일 승자)
- `CompareAndSetState(id, expected, next)`:
  `db.scans.findOneAndUpdate({_id:id, state:expected}, {$set:{state:next, updatedAt:now}})` — 매치 문서 유무로 `(swapped bool, err)` 판정.
- `CompareAndSetItemState(wid, expected, next)`:
  `db.workledger.findOneAndUpdate({_id:wid, state:expected}, {$set:{state:next}})` — 동일. **32-worker 동시 claim에서 정확히 1명 승자**(mem의 `TestMem_ItemCAS_SingleWinner`를 계약 스위트로 승격해 Mongo에도 실행, Q10=A).
- 단일 문서 원자성이라 **멀티도큐먼트 트랜잭션 불필요** → 단일 노드에서도 정확하지만, 내구성(Q12=B)을 위해 replica set을 채택.

### 2.5 내구성 / 백업 (Q12=B, RESILIENCY-12)
- **RPO ≈ 0**: `workledger`/`scans`/`reports` write는 **write-concern `majority`** (완료된 WorkItem 결과 무손실). majority는 replica set(Q6=B)이 전제 → Q6=B와 Q12=B가 상호 강제.
- **RTO ≈ 수 분**: primary 장애 시 자동 election(초 단위) + manager 재기동. 진행 중 스캔은 `ResumeScan`으로 이어받음(재실행 불가, Q12=B).
- **백업**: 지속 oplog + 일 1회 스냅샷/`mongodump`, 7일+ 보관, 백업 암호화(RESILIENCY-12/SECURITY-01). 시점 복구(PITR) 가능.
- **검증**: 백업 무결성은 분기별 테스트 복원(RESILIENCY-12, Operations로 시나리오 이월).

---

## 3. Messaging — Valkey (WorkQueue, Redis Streams)

### 3.1 배포 형태 (Q8=A, Q9=C)
- **Valkey** (Redis 7.2 BSD 포크). `go-redis` 클라이언트 호환. RSAL/SSPL 라이선스 리스크 없음.
- **큐 전용 인스턴스** (Q9=C): `ResponseCache`(#5, 휘발성 캐시)와 **분리**. 이유: 큐는 내구성 필요(AOF `appendfsync everysec`+), 캐시는 휘발성(`allkeys-lru` eviction) — 메모리/eviction 정책이 정반대라 한 인스턴스에 두면 캐시 부하가 큐 항목을 evict할 위험.
- **HA**: Valkey primary + replica(같은 replica set 개념) 또는 Sentinel, 멀티AZ. 큐 유실은 RPO에 영향 없음(원장이 SoT) — 단 진행 중 배달은 재개로 복구.
- **전송 암호화**(SECURITY-01): `tls=true`. **인증**(SECURITY-09): `requirepass`/ACL, 기본 접근 차단. **최소 권한**(SECURITY-06): chiller ACL은 `chiller:queue:*` 키 접두사 + 필요한 스트림 명령만.

### 3.2 배달 방식 (Q7=B + C2=A: Streams, seam 불변)

`WorkQueue` 3-메서드 seam(`Enqueue`/`Dequeue`/`Drain`)을 **바꾸지 않고** Streams 채택:

> **큐 모델 = 단일 공유 스트림 (Q2=B와 정합)**: worker가 스캔당 Job이 아니라 **상주 오토스케일 풀**(Q2=B)이므로, 큐도 **하나의 공유 스트림 `chiller:queue:stream` + consumer group `workers`**(컨슈머 = 풀 replica들)로 둔다. 이것이 Streams consumer group의 정석 사용(안정적 소비자 풀이 메시지 경쟁 소비)이며, 스캔당 스트림 키(이전 설계)는 상주 풀이 동적 스트림 집합을 `XREADGROUP`하기 어려워 폐기.

| seam 메서드 | Valkey 구현 |
|---|---|
| `Enqueue(ctx, item)` | `XADD chiller:queue:stream * item <payload>` (payload = 직렬화 WorkItem, scanId 포함) |
| `Dequeue(ctx)` | `XREADGROUP GROUP workers <consumer=podname> BLOCK <ctx-derived> COUNT 1 STREAMS chiller:queue:stream >` — ctx 취소 시 BLOCK 타임아웃 루프로 언블록(mem의 `TestMem_Dequeue_CtxCancelUnblocks` 계약 승격). **drained 스캔 항목은 impl 내부에서 즉시 `XACK`하고 건너뜀**(아래) → 워커 루프·seam 불변 |
| `Drain(ctx, scanId)` | 공유 스트림이라 `DEL` 불가. **`SADD chiller:queue:drained <scanId>`** 로 drained 표식만. 미배달 항목은 스트림에 남지만, `Dequeue`가 drained 멤버 항목을 읽으면 claim 없이 `XACK`+skip(lazy drain) → 체커에 절대 디스패치되지 않음(seam 계약 "미배달 항목 취소" 충족). scan 종료 시 표식 정리 |

**C2=A — Ack/회수를 seam 밖 내부 reaper로 흡수**:
- `Dequeue`가 `XREADGROUP`으로 받으면 그 엔트리는 컨슈머 PEL(pending)에 들어감.
- **배경 reaper 고루틴**(worker 프로세스 내부, seam 밖): 주기적으로 `XAUTOCLAIM`으로 idle-timeout 초과 pending 엔트리를 회수 → 재배달. 이미 원장 CAS(PR-4)로 claim/완료된 item은 `XACK`만 하고 버림(재배달돼도 CAS가 중복 거름 = at-least-once + visibility를 CAS로 흡수, E-6 규칙 5).
- **XACK 시점**: 워커가 원장 `CompareAndSetItemState(pending→inflight)` 성공 후 처리를 끝내고 `RecordOutcome`까지 커밋하면 그 엔트리를 `XACK`. reaper는 "원장상 terminal인데 PEL에 남은" 엔트리를 청소.
- Ack 신호가 **원장 상태 + reaper 내부**에 있으므로 `WorkQueue` 인터페이스 **불변**(PR-11 준수). Q7=B의 취지("재개 경로 비의존")도 유지 — 크래시 회수를 `XAUTOCLAIM`이 처리(`ResumeScan`에만 의존하지 않음).

> **주의(SECURITY-15 fail-closed)**: reaper가 죽어도 안전 측 실패 — 회수 안 된 pending은 `ResumeScan`+`ListUnfinished`(원장 inflight 재적재)가 여전히 백업 경로. 두 경로가 다중 방어(RESILIENCY-10).

---

## 4. 공급망 (SECURITY-10, F-6)

신규 직접 의존 2개 추가 → 다음 규율 적용:
- 버전 **정확히 핀** (`go.mod` + `go.sum` 락, 이미 커밋됨): `go.mongodb.org/mongo-driver` (또는 `mongo-driver/v2`), `github.com/redis/go-redis/v9` (Valkey 호환), 테스트용 `github.com/testcontainers/testcontainers-go`.
- **취약점 스캔**: `govulncheck`를 CI 단계에 추가(build-and-test 지시서에 반영). 
- **불용 의존 금지** / 공식 레지스트리만.
- **SBOM**: 프로덕션 이미지에 대해 생성(`syft` 또는 `go`-native).
- **이미지 태그 핀**(SECURITY-10/13): Dockerfile은 `mongo:<pinned-digest>`, `valkey/valkey:<pinned-digest>` — `latest` 금지. base 이미지도 digest 핀.

---

## 5. 관측 / 헬스 (RESILIENCY-05/06)

- **Deep health check**(RESILIENCY-06): manager `/healthz`는 Mongo `ping` + Valkey `PING`을 확인. 하나라도 실패 → unhealthy → LB/스케줄러가 트래픽 차단. **부팅 게이트**(PR-9)와 정합: 백엔드 연결 실패 시 스캔 시작 거부(SECURITY-15 fail-closed).
- **Shallow health**: `/livez` 프로세스 생존만.
- **메트릭**: 큐 깊이(`XLEN`), 원장 pending 수, CAS 경합률, 백엔드 호출 지연/에러율 — RESILIENCY-05. (OTel exporter 자체는 infra-plan #3, 별도 스코프 — 여기선 헬스 엔드포인트와 메트릭 노출 지점만 설계.)
- **Introspection UI (2026-07-11, 사용자 요청)**: manager가 읽기 전용 대시보드 `/ui` + `/api/introspect/{queue,scans}` 를 서빙 — 큐 깊이·pending·consumer lag·drained·스캔별 진행(ProgressSnapshot)을 asynqmon류로 시각화(asynq 미채택, self-contained). 큐 통계는 **별도 선택적 인터페이스 `QueueInspector.Stats`**(Valkey `XPENDING`/`XINFO`)로 노출 — **`WorkQueue` 3-메서드 seam은 불변**(PR-11). UI가 HTML을 서빙하므로 **SECURITY-04 보안 헤더가 이제 적용**(CSP/HSTS 등, compliance-summary 갱신), authz fail-closed(SECURITY-08).
- **로깅**(SECURITY-03): 백엔드 호출 실패는 구조적 로그(correlation=ScanID/WorkItemID), 시크릿·토큰 미기재.

---

## 6. 회복탄력성 상호작용 (RESILIENCY-10)

- **타임아웃**: 모든 Mongo/Valkey 호출에 명시적 ctx 데드라인(무한 대기 금지). `Dequeue`의 BLOCK만 의도적 장기 블록(ctx로 취소 가능).
- **연결 풀 한계**(bulkhead): Mongo/Valkey 클라이언트 풀 상한 설정 → 한 백엔드 지연이 전체 고루틴 고갈로 번지지 않게.
- **서킷/degrade**: 백엔드 불가 시 신규 스캔 거부(fail-closed), 진행 중 스캔은 재개 대기. 부분 실패 격리(PR-4b)는 이미 구현.

---

## 7. 데이터 계약 요약 (P1-4)

```
scans        : _id=ScanID   { state:str, request, frameworks[], createdAt, updatedAt, report:null }
               idx: {state:1}
workledger   : _id=WorkItemID { scanId, item{ id,scanId,scope,target{url,host,scheme} }, state:str, outcome:null|{...} }
               idx: {scanId:1, state:1}
reports      : _id=ScanID   { ...report body... }
queue stream : key=chiller:queue:stream (single shared)  group=workers  consumers=pool replicas
               entry={ WorkItem payload incl scanId }
               Enqueue=XADD ; Dequeue=XREADGROUP ; Ack=XACK after ledger terminal
               recovery=XAUTOCLAIM (reaper) ; Drain=SADD chiller:queue:drained <scanId> + lazy XACK-skip
```
- **마이그레이션 정책**: enum 문자열화(Q4=A) 덕에 필드 추가는 하위호환. 스키마 버전 필드 `schemaVersion`를 `scans`에 두어 향후 변환 지점 표시(현재 v1).
- **write-concern**: `scans`/`workledger`/`reports` = `majority`(Q12=B). 읽기 = `primary`(CAS 정확성).

---

## 8. 이번 스코프에서 **제외**(명시)

- `ProgressReporter`→OTel(#3), `Authorizer`→OIDC/mTLS(#4), `ResponseCache`→Valkey(#5), `FindingsStore`→Mongo(#6): infra-plan의 다음 항목. 단, #5/#6이 **이 설계의 인스턴스 배치(§공유 인프라)** 를 공유하므로 배치 결정만 선반영(shared-infrastructure.md).
- 금융 HTTPS/MITM 하드닝(§2), probe 바이너리(§3), PlaywrightDiscoverer(§4): 무관.

배포 토폴로지·다이어그램·배포/롤백 절차는 `deployment-architecture.md` 참조.
확장 규칙 컴플라이언스 표는 `compliance-summary.md` 참조.
