# Code Generation Plan — platform (U1) Infrastructure Increment

**Stage**: CONSTRUCTION → Code Generation (per-unit, platform U1 — infra seam-swap 증분)
**Scope**: Infrastructure Design 승인본 구현 — `TaskStore`→Mongo, `WorkQueue`→Valkey(Streams). seam 인터페이스 불변(PR-11).
**Single source of truth**: 이 문서가 이 증분의 유일한 코드 생성 기준. 기존 MVP 플랜(`platform-code-generation-plan.md`)과 별개.
**Design inputs**: `construction/platform/infrastructure-design/{infrastructure-design,deployment-architecture,compliance-summary}.md` · `shared-infrastructure.md` · `functional-design/{domain-entities,business-rules}.md`(E-5/E-6/PR-4/PR-11).
**Locked decisions**: Q1=C·Q2=B·Q3=A·Q4=A·Q5=A·Q6=B·Q7=B·Q8=A·Q9=C·Q10=A·Q11=C·Q12=B·C1=A·C2=A.

---

## Unit Context

- **구현 대상 stories**: US-025(워커 루프·부분 실패 격리), US-027(중단·재개), + 인프라 비기능(내구성·확장·관측). 기능 로직은 이미 구현됨 — 이 증분은 **저장/큐 백엔드 교체**.
- **의존**: domain(U0, 영속 타입), findings(U3, report.Report). 두 유닛 green. **인터페이스 불변**이라 orchestrator/api/proxy/scan 코드 무변경.
- **소유 엔티티**: `platform.Scan`, `WorkLedgerEntry`, `ProgressSnapshot`; 영속 대상 domain 타입(WorkItem/WorkItemOutcome/Target/ScanRequest).
- **경계**: 새 impl은 `taskstore`/`workqueue` 패키지 안에 mem impl **옆에** 추가. 선택은 `cmd/chiller`에서만(PR-11b).

## 코드 위치 (workspace root, NEVER aidlc-docs/)
- `internal/platform/taskstore/` · `internal/platform/workqueue/` · `internal/platform/api/`(health) · `internal/domain/` · `internal/platform/`(types) · `cmd/chiller/` · `deploy/` · `go.mod`
- 문서 요약만: `aidlc-docs/construction/platform/code/`

---

## Steps (번호순, 체크박스 — 완료 즉시 [x])

### Step 1 — 의존성 추가 (SECURITY-10)
- [x] 1a: `go get` 버전 핀 — `go.mongodb.org/mongo-driver/v2` (또는 v1 안정), `github.com/redis/go-redis/v9`, 테스트용 `github.com/testcontainers/testcontainers-go`(+ mongodb/redis 모듈)
- [x] 1b: `go mod tidy` → `go.mod`/`go.sum` 락 커밋. 불용 의존 없음 확인
- [x] 1c: `make arch-check` 계층 규칙에 신규 import 경로 위반 없는지 확인(domain은 여전히 인프라 무의존 — 태그만 예외)

### Step 2 — 직렬화 정책 (Q4=A, F-3/F-4) — Business Logic
- [x] 2a: 영속 타입에 `bson:"..."` 태그 추가 — `domain.WorkItem`, `domain.WorkItemOutcome`, `domain.Target`, `ingest.ScanRequest`/`ScanOptions`, `platform.Scan`, `platform.WorkLedgerEntry`, `platform.ProgressSnapshot`. (도메인 순수성 유지: 태그만, 로직 없음)
- [x] 2b: `ScanState`·`ItemState`에 문자열 BSON marshaling — `MarshalBSONValue`/`UnmarshalBSONValue`(문자열 저장) + 역파서 `parseScanState`/`parseItemState`(기존 `String()` 재사용). int iota 저장 금지(F-4)
- [x] 2c: `NotApplicable map[CheckerID][]CWE` 등 map 키 직렬화 확인(CheckerID=string OK)
- [x] 2d: 요약을 `platform/code/`에 기록(직렬화 정책)

### Step 3 — 계약 스위트 추출 (F-2, P2-1) — Business Logic Unit Testing
- [x] 3a: `internal/platform/taskstore/contract_test.go` — `func runTaskStoreContract(t *testing.T, newStore func() platform.TaskStore)` 로 기존 4개 테스트(ItemCAS 단일승자·StateCAS 가드·Unfinished/Outcomes·Resume 미회수) 이관
- [x] 3b: `internal/platform/taskstore/taskstore_test.go` 를 `runTaskStoreContract(t, func() platform.TaskStore { return New() })` 호출로 축소(mem)
- [x] 3c: `internal/platform/workqueue/contract_test.go` — `func runWorkQueueContract(t, newQueue func() platform.WorkQueue)` 로 4개(FIFO·블록·ctx취소·Drain) 이관
- [x] 3d: `workqueue_test.go` 를 mem 호출로 축소
- [x] 3e: **PBT 추가**(FULL blocking): 상태 기반(PBT-06) — 명령 시퀀스(enqueue/dequeue/claim/record/drain) 를 mem 오라클(PBT-05)과 비교; 직렬화 왕복(PBT-02) `Unmarshal(Marshal(x))==x`; `rapid` 사용(PBT-09), 도메인 생성기(PBT-07), seed 로깅(PBT-08)
- [x] 3f: mem 경로 `go test ./internal/platform/...` green 확인(백엔드 추가 전, 계약 스위트가 mem에 대해 통과)

### Step 4 — TaskStore Mongo impl (Q5=A/Q6=B/Q12=B) — Repository Layer
- [x] 4a: `internal/platform/taskstore/taskstore_mongo.go` — `type Mongo struct{ … }`, `func NewMongo(ctx, uri, db string) (*Mongo, error)` (연결·인덱스 보장·majority write-concern)
- [x] 4b: 11개 메서드 구현 — SaveScan/LoadScan/AppendWorkItems/RecordOutcome/ListUnfinished/AllItems/AllOutcomes/SaveReport/LoadReport
- [x] 4c: **CAS 2종** — `CompareAndSetState`/`CompareAndSetItemState` = `findOneAndUpdate({_id,state:expected},{$set:{state:next}})` 매치 유무로 `(bool,error)` (PR-4 단일 승자)
- [x] 4d: 컬렉션 `scans`/`workledger`/`reports`, 인덱스 `{state:1}`·`{scanId:1,state:1}`; `schemaVersion` 필드
- [x] 4e: `var _ platform.TaskStore = (*Mongo)(nil)` 컴파일 assertion
- [x] 4f: fail-closed 에러 처리(SECURITY-15): 연결/쿼리 실패는 error 전파, 조용한 성공 금지

### Step 5 — WorkQueue Valkey impl (Q7=B/Q8=A/C2=A) — Repository Layer
- [x] 5a: `internal/platform/workqueue/workqueue_valkey.go` — `type Valkey struct{ … }`, `func NewValkey(ctx, addr string, opts …) (*Valkey, error)` (consumer group `workers` 보장 `XGROUP CREATE MKSTREAM`)
- [x] 5b: `Enqueue`=`XADD chiller:queue:stream`; `Dequeue`=`XREADGROUP … BLOCK` + ctx 취소 루프 + **drained 멤버 즉시 XACK+skip**(lazy drain); `Drain`=`SADD chiller:queue:drained`
- [x] 5c: **내부 reaper 고루틴**(C2A) — 주기적 `XAUTOCLAIM` idle pending 회수·재배달; 원장상 terminal이면 `XACK`만. `Close()`로 중단
- [x] 5d: XACK 시점 = 워커가 처리 커밋 후(호출부는 seam 밖 — impl이 outcome 반영 지점을 콜백/헬퍼로 노출하되 인터페이스 불변 유지; 설계상 워커 루프가 Dequeue→처리→(내부)ack. **주의: 워커 루프 코드 무변경 원칙 재확인** — ack은 다음 Dequeue의 XREADGROUP `>`가 아니라 명시 XACK 필요하므로, Valkey impl이 "이전에 넘긴 엔트리 id를 기억했다가 다음 Dequeue 또는 별도 시점에 XACK"하는 방식으로 캡슐화)
- [x] 5e: noeviction·AOF 전제 문서화(운영), `var _ platform.WorkQueue = (*Valkey)(nil)`
- [x] 5f: fail-closed 에러 처리

### Step 6 — Composition root 배선 (F-5, PR-11b) — API/Wiring Layer
- [x] 6a: `cmd/chiller/main.go` 하드 가드(75-77) 제거 → `switch f.store { case "mem": …; case "mongo": NewMongo(…) }`, `switch f.queue { case "mem": …; case "valkey": NewValkey(…) }`
- [x] 6b: 신규 플래그 — `--mongo-uri`, `--mongo-db`, `--valkey-addr`(+ 시크릿은 env 우선, SECURITY-12), 기본값은 mem 유지(하위호환)
- [x] 6c: 잘못된 조합/연결 실패 시 부팅 거부(SECURITY-15, 부팅 게이트 PR-9 정합)

### Step 7 — Deep health check (RESILIENCY-06) — API Layer
- [x] 7a: `internal/platform/api` 에 `/healthz`(Mongo ping+Valkey PING)·`/livez` 추가 또는 기존 확장. 백엔드 미구성(mem)일 땐 항상 healthy
- [x] 7b: 단위 테스트(fake backend healthy/unhealthy)

### Step 8 — Introspection UI (큐 가시성, 사용자 요청) — API/Frontend Layer
- [x] 8a: **선택적 인터페이스** `internal/platform` 에 `QueueInspector interface { Stats(ctx) (QueueStats, error) }` + `QueueStats{ Depth, Pending, Drained int; Consumers []ConsumerStat{Name,Pending,IdleMs} }` 정의. **`WorkQueue` 3-메서드는 불변**(PR-11) — Inspector는 별개 인터페이스
- [x] 8b: Valkey impl에 `Stats` 구현(`XLEN`·`XPENDING`·`XINFO GROUPS/CONSUMERS`·`SCARD drained`); mem impl에도 구현(내부 카운터). reaper 회수 카운터도 노출
- [x] 8c: manager에 읽기 전용 introspection JSON 엔드포인트 — `/api/introspect/queue`(QueueInspector 있으면 Stats, 없으면 n/a), `/api/introspect/scans`(TaskStore 기존 메서드로 스캔별 ProgressSnapshot: Total/Done/Failed/Inflight + PerChecker). **입력 검증**(SECURITY-05), **authz fail-closed**(SECURITY-08)
- [x] 8d: **정적 대시보드** `/ui` — 의존성 없는 단일 HTML+JS(외부 CDN 없음), 위 JSON을 폴링해 큐 깊이·pending·consumer lag·drained·스캔별 진행을 표로. asynqmon류 가시성을 self-contained로
- [x] 8e: **HTML 서빙 보안 헤더**(SECURITY-04, 이제 적용) — CSP(`default-src 'self'`)·HSTS·X-Content-Type-Options=nosniff·X-Frame-Options=DENY·Referrer-Policy 미들웨어. 로그/화면에 시크릿·토큰 미노출(SECURITY-03)
- [x] 8f: 단위 테스트 — Stats(mem), introspect 엔드포인트(authz 거부/허용·헤더 존재), UI 라우트 200

### Step 9 — 통합 테스트 (Q10=A, testcontainers) — Repository Layer Testing
- [x] 9a: `internal/platform/taskstore/taskstore_mongo_test.go` (`//go:build integration`) — Mongo 컨테이너 기동 후 `runTaskStoreContract(t, mongoCtor)` 실행
- [x] 9b: `internal/platform/workqueue/workqueue_valkey_test.go` (`//go:build integration`) — Valkey 컨테이너 후 `runWorkQueueContract` + reaper/XAUTOCLAIM 회수 시나리오 + `Stats` 검증
- [x] 9c: 통합 스위트는 기본 CI에서 제외(tag), 별도 잡에서 실행. mem 계약 스위트는 항상 실행(PBT-05 오라클 동일 스위트)

### Step 10 — 배포 자산 (P2-6) — Deployment Artifacts
- [x] 10a: `Dockerfile`(멀티스테이지, base·태그 digest 핀, non-root, SECURITY-09/10)
- [x] 10b: `deploy/compose/docker-compose.yml` — chiller(all)+Mongo(single dev)+Valkey(queue)+Valkey(cache)
- [x] 10c: `deploy/k8s/` — manager Deployment(1)·proxy Deployment(1)·worker Deployment+HPA(큐 깊이)·Mongo StatefulSet(3)·Valkey(큐/캐시)·Secret·NetworkPolicy(SECURITY-07)
- [x] 10d: 이미지·매니페스트에 `latest` 금지·digest 핀(SECURITY-10/13)

### Step 11 — CI/문서 (SECURITY-10, RESILIENCY-04)
- [x] 11a: `Makefile`/CI에 `govulncheck` 타깃, `integration` 잡, SBOM 생성 단계 추가
- [x] 11b: `construction/build-and-test/` 지시서 갱신(통합 테스트·컨테이너·취약점 스캔·UI)
- [x] 11c: `aidlc-docs/construction/platform/code/infra-business-logic-summary.md` — 구현 요약(seam-swap 결과·CAS·Streams reaper·직렬화·UI·배포)

### Step 12 — 검증 (green 기준)
- [x] 12a: `go build ./...` · `go vet ./...` · `gofmt -l`(clean) · `make arch-check`(OK)
- [x] 12b: `go test ./...`(mem 경로 + 계약 스위트 + PBT + UI) all pass, `go test -race ./internal/platform/...`
- [x] 12c: (Docker 가용 시) `go test -tags integration ./internal/platform/...` — Mongo/Valkey 컨테이너 계약 통과
- [x] 12d: 결과를 요약 문서·audit·state에 반영

---

## 확장 규칙 준수 체크(생성 단계)
- **PBT** (FULL, blocking): Step 3e에서 상태 기반(PBT-06)·왕복(PBT-02)·오라클(PBT-05) 필수. mem을 오라클로 백엔드 등가 검증.
- **SECURITY**: 10(의존 핀/SBOM/스캔, Step 1·10·11)·12(시크릿 env, Step 6)·15(fail-closed, Step 4f/5f/6c)·07/09(배포, Step 10)·**04(HTML 보안 헤더, Step 8e — UI 추가로 이제 적용)**·05(입력검증, Step 8c)·08(authz, Step 8c).
- **RESILIENCY**: 06(health, Step 7)·09(HPA, Step 10c)·10(타임아웃·풀 한계, Step 4/5)·12(백업 문서, Step 11c). 05(관측 UI, Step 8).

## Story traceability
- US-025(워커 루프·격리) → Step 5(큐 배달·reaper)·Step 4(CAS claim)
- US-027(중단·재개) → Step 4(ListUnfinished/CAS)·Step 5(drained·XAUTOCLAIM)
- 인프라 비기능(내구성·확장·관측) → Step 4(majority)·Step 5(Streams)·Step 7(health)·Step 8(introspection UI)·Step 10(HPA/멀티AZ)

## 총 규모
12 steps. 새 파일 ~14개(2 impl + 2 contract + 2 integration + health + QueueInspector + introspect endpoints + static UI + Dockerfile + compose + k8s + summary), 수정 ~7개(도메인/플랫폼 타입 태그, cmd/chiller, api 라우팅/헤더, 기존 2 test 축소, Makefile). **`WorkQueue` 3-메서드 seam 불변**(QueueInspector는 별개 인터페이스) → orchestrator/scan/proxy 무변경.
