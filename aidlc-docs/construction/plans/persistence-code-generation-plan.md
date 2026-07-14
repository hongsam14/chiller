# Code Generation Plan — persistence 증분 (findings U3 + scan U2)

**Stage**: CONSTRUCTION → Code Generation (Part 1: Planning)
**Created**: 2026-07-13
**설계 근거 (승인됨 2026-07-13)**: `construction/persistence/infrastructure-design/{infrastructure-design,deployment-architecture,compliance-summary}.md`
**확정 답변**: Q1=A · Q2=A · Q3=A · Q4=A · Q5=A · Q6=A · Q7=C · Q8=A · Q9=A · Q10=A · Q11=A · Q12=A · C1=A · C2=B

> **이 플랜이 Code Generation의 유일한 단일 진실원(single source of truth)이다.**
> 각 단계 완료 즉시 체크박스를 `[x]`로 갱신한다 (Plan-Level Checkbox Enforcement).
> 플랜에 없는 것은 만들지 않는다 (NO HARDCODED LOGIC).

---

## 1. 프로젝트 컨텍스트

| 항목 | 값 |
|---|---|
| Project type | **Brownfield** (기존 Go 모듈 `github.com/enki-offen/chiller`) |
| Application code | **워크스페이스 루트** (`internal/`, `cmd/`, `deploy/`, `configs/`) — **절대 `aidlc-docs/`에 두지 않는다** |
| Documentation | `aidlc-docs/construction/persistence/code/` (마크다운 요약만) |
| 파일 규율 | 기존 파일은 **in-place 수정**. `store_new.go` 같은 **복제본 금지** |
| 아키텍처 가드 | `make arch-check` (domain은 상위 레이어를 import하지 않는다) |
| 검증 기준 | `go build ./...` · `go vet ./...` · `gofmt -l` clean · `go test ./...` · `go test -race ./...` · `make arch-check` **전부 green** |

### 유닛 컨텍스트 / 스토리 추적

| 스토리 | 이 증분에서의 관련성 |
|---|---|
| **US-002** — 단일 요청 재사용 (cross-family, ResponseCache) | `ResponseCache`가 프로세스 경계를 넘어 실제로 재사용되게 함 (Valkey). **FR-02**가 manager/worker 분리 토폴로지에서 처음으로 성립 |
| **US-022** — 최소 증거 보존 | evidence를 **암호화**해 durable 저장. `MaxEvidenceSnippets` 경계는 저장·로드 라운드트립에서도 유지 |
| **US-019** — 통합 컴플라이언스 리포트 | `Report.FrameworkLabels`가 인덱스 → **FindingKey** 참조로 변경 (C2=B). 리포트 조립이 두 저장소 조인으로 바뀜 |
| **US-023** — 상태·리포트 API | 리포트 읽기 경로가 `TaskStore`(골격) + `FindingsStore`(본문) 조인으로 변경. 신규 introspect 엔드포인트 |
| **FR-12 / NFR-16** — 응답 본문 폐기 | **`WipeScan` 미배선 결함 해소** (현재 FR-12 미충족) |

### 유닛 의존성

- `domain` (U0, kernel) — `Finding`/`Evidence`/`HAREntry`/`Severity`/**신규 `FindingKey`** 소유. 상위 레이어를 import하지 않는다.
- `findings` (U3) — `store` 패키지(`FindingsStore`), `report` 패키지(`FrameworkLabels`).
- `scan` (U2) — `fetch` 패키지(`ResponseCache`).
- `platform` (U1, composition root) — `orchestrator`(finalize/fail/stop 배선), `taskstore`(report 골격 저장), `api`(introspect/healthz), `cmd/chiller`(조립).

---

## 2. 실행 단계

### Phase A — 도메인 커널 (U0) : 직렬화 계약 + 키 + 암호화 원시

**Step 1 — `domain` 직렬화 태그 + int-iota enum **전수** 문자열 마샬링 (Q4=A, **G2=A로 범위 확대**)** `[x]`
- 수정: `internal/domain/finding.go` — `Finding`·`Evidence`에 명시적 `bson:` 태그
- 수정: `internal/domain/har.go` — `HAREntry`·`Header`·`Cookie`에 `bson:`/`json:` 태그
- 수정: `internal/domain/severity.go` — `Severity` 문자열 마샬링 → `"info"|"low"|"medium"|"high"|"critical"`
- **수정: `internal/domain/coverage.go` — `ItemStatus` 문자열 마샬링 + `ItemRecord`에 bson 태그** (**G2**)
- **수정: `internal/domain/ids.go` — `ScopeKind` 문자열 마샬링** (**G2**)
- 세 enum 모두 `bson.ValueMarshaler`/`ValueUnmarshaler` + `MarshalJSON`/`UnmarshalJSON` + **파서**(문자열→enum). **미지 문자열은 에러**(조용한 기본값 금지)
- **근거**: 설계 §3.3 + **G2**. 세 enum 전부 이미 int로 Mongo에 저장 중(`reports…severity`, `reports…coverage[].status`, `workledger.item.scope`). 특히 **`StatusChecked`가 iota 0번**이라 **필드 누락 문서가 "검사 완료"로 읽힌다**(BSON 기본값 0) → 컴플라이언스 **미탐**. 프로덕션 데이터가 없는 지금이 유일하게 싼 시점

**Step 2 — `domain.FindingKey` (C2=B)** `[x]`
- 수정: `internal/domain/finding.go` — `type FindingKey string` + `func (f Finding) Key() FindingKey` = `sha256hex(IdentityKey())`
- **근거**: 설계 §3.5. `IdentityKey()`의 `0x1f` 구분자를 인코딩에 싣지 않기 위해 해시

**Step 3 — 봉투 암호화 원시 (`internal/crypto`, 신규 패키지)** `[x]`
- 생성: `internal/crypto/envelope.go` — AES-256-GCM 봉투
  - `type Envelope struct { V int; KeyID string; Nonce []byte; CT []byte }` (bson/json 태그)
  - `type Keyring interface { Current() (id string, key []byte); Get(id string) ([]byte, bool) }`
  - `func NewKeyring(env string) (Keyring, error)` — `CHILLER_EVIDENCE_KEY` (base64 32B) 파싱, **길이 검증 실패 시 에러**
  - `Seal(kr, plaintext, aad []byte) (Envelope, error)` / `Open(kr, e Envelope, aad []byte) ([]byte, error)`
  - nonce는 **쓰기마다 `crypto/rand`**로 생성
- **근거**: 설계 §3.6/§4.1. **신규 의존성 0** (표준 `crypto/aes`+`crypto/cipher`)
- **레이어**: `internal/crypto`는 domain을 import하지 않는 순수 유틸 → arch-check 안전

**Step 4 — Phase A 테스트** `[x]`
- `internal/domain/severity_test.go` · `coverage_test.go` · `ids_test.go` — **세 enum 각각** 문자열 라운드트립, **미지 문자열 거부**, **필드 누락 문서가 조용히 0-값으로 읽히지 않음**(G2의 핵심 위험)
- `internal/domain/finding_test.go` — `Key()` 안정성(같은 finding → 같은 키), CWE 순서 무관성(정규화 후)
- `internal/crypto/envelope_test.go` — **PBT**: `Open(Seal(x)) == x` 항등 / **AAD 불일치 시 반드시 실패**(봉투 이식 차단) / 변조된 CT는 복호화 실패(AEAD 무결성, SECURITY-13) / nonce 재사용 없음
- **PBT-02(라운드트립), PBT-03(불변식)**

---

### Phase B — findings 저장 (U3)

**Step 5 — `FindingsStore` 계약 정정 (Q2=A) + 계약 스위트 추출 (Q10=A)** `[x]`
- 수정: `internal/findings/store/store.go` — 인터페이스에 `ctx context.Context` 추가 (`SaveFindings(ctx,…)`, `LoadFindings(ctx,…)`), `InMemory` 구현 갱신
- 생성: `internal/findings/store/contract_test.go` — **생성자 테이블** 계약 스위트 (platform의 `taskstore/contract_test.go` 패턴 그대로)
  - 라운드트립 · **순서 보존**(설계 §2.1 신규 계약) · 없는 스캔 = 빈 결과 · 재저장 멱등 · **꼬리 삭제**(이전 실행이 더 많은 finding을 남겼을 때 유령 제거) · 호출자 슬라이스 변이 격리
- 기존 `store_test.go`의 개별 테스트는 계약 스위트로 흡수 (중복 제거)

**Step 6 — `store.Mongo` (Q1=A, Q5=A, Q7=C)** `[x]`
- 생성: `internal/findings/store/store_mongo.go`
  - `NewMongo(ctx, uri, dbName string, kr crypto.Keyring, ttl time.Duration) (*Mongo, error)` — 연결 + `Ping`(fail-closed) + `EnsureIndexes`, majority write concern
  - 문서 스키마: `{_id: "<scanId>\x1f<idx>", scanId, idx, key, cwes, targetUrl, sourceTool, severity, evidence: Envelope, schemaVersion: 1, createdAt}`
  - 인덱스: `{scanId:1, idx:1}` · `{scanId:1, key:1}` · `ttl>0`이면 `{createdAt:1}` + `expireAfterSeconds`
  - `SaveFindings`: bulk `ReplaceOne(upsert)` + **`deleteMany({scanId, idx: {$gte: len}})`** (꼬리 절단)
  - `LoadFindings`: `find({scanId}).sort({idx:1})` → evidence 복호화 → `[]domain.Finding`
  - evidence 암호화: `crypto.Seal(kr, JSON(snippets), aad=scanId+key)`
  - `Ping(ctx) error` (헬스체크용)
- **근거**: 설계 §3.1/§3.2/§3.6/§3.7
- **G6**: **`store.InMemory`는 암호화하지 않는다** — 키링은 Mongo 구현에만 주입 (설계 §3.6)
- **레이어 주의**: `internal/findings/store`가 `internal/crypto`를 import — arch-check 규칙 확인 필요

**Step 6B — 🔴 원장(`workledger`)의 evidence 봉투 암호화 (G1′=E ①) — 설계 누락분** `[x]`
- 수정: `internal/platform/taskstore/taskstore_mongo.go`
  - `RecordOutcome` 시 `outcome.findings[].evidence`를 `crypto.Seal` (AAD = `scanId` + `itemId` + `findingKey`)
  - `AllOutcomes` / `AllItems` 등 원장 읽기 경로에서 `crypto.Open`
  - `taskstore.Mem`은 **암호화하지 않는다** (프로세스 메모리)
  - `NewMongo`에 `crypto.Keyring` 주입 (기존 시그니처 확장)
- **근거**: **data-census D1** — 원장이 findings의 전송 경로라서 evidence가 `workledger`에 **평문 durable 저장 중**이었다. `findings` 컬렉션만 암호화하면 SECURITY-01은 **여전히 미충족**. 규칙 **O-4**(민감 데이터는 소유 싱크에서만)
- **주의**: 이건 platform(U1) 코드지만 **findings의 데이터 계약 문제**라 Phase B에 둔다

**Step 7 — findings 저장 재시도 (Q9=A)** `[x]`
- 생성: `internal/findings/store/retry.go` — `WithRetry(inner FindingsStore, attempts int, base time.Duration) FindingsStore` 데코레이터
  - 3회, 100ms→400ms→1.6s + jitter, `ctx` 취소 시 즉시 중단
  - **재시도 대상은 일시적 오류만** — `context.Canceled`/`context.DeadlineExceeded`·중복키·검증 실패는 **즉시 실패**
  - 카운터 노출: `findings_save_retries_total`, `findings_save_failures_total`
- **근거**: 설계 §3.8. 데코레이터라 mem/Mongo 양쪽에 무차별 적용 가능

**Step 8 — Phase B 테스트** `[x]`
- `contract_test.go` 확장: Mongo 생성자를 테이블에 추가 (integration 태그에서만 활성)
- `store_pbt_test.go` — **PBT**: `Save→Load` 항등(**순서·CWE 정규화·`MaxEvidenceSnippets` 경계 보존**), 멱등(N회 저장 = 1회 저장)
- **`taskstore` 원장 암호화 테스트 (Step 6B)** — `RecordOutcome→AllOutcomes` 라운드트립 항등 / **저장된 BSON 문서에 evidence 평문이 존재하지 않음**을 단언(raw 문서 조회) / AAD 불일치 거부
- `retry_test.go` — 일시 오류 재시도 후 성공 / 영구 오류 즉시 실패 / ctx 취소 시 중단
- **PBT-02/03/04**

---

### Phase C — 리포트 계약 변경 (U3) + report/findings 경계

**Step 9 — `Report.FrameworkLabels` → `FindingKey` (C2=B)** `[x]`
- 수정: `internal/findings/report/report.go` — `FrameworkLabels map[domain.Framework]map[string][]domain.FindingKey` (기존 `[]int`), `Builder.Build`가 인덱스 대신 `f.Key()` 수집
- 수정: `internal/findings/report/report_test.go` — 인덱스 단언을 키 단언으로
- 리포트에 `SchemaVersion int` 추가 (외부 소비자가 계약 변경을 구분)
- **근거**: 설계 §3.5. 순서 의존성을 구조적으로 제거

**Step 10 — `TaskStore`의 report 저장에서 findings 분리 (Q1=A)** `[x]`
- 수정: `internal/platform/taskstore/taskstore_mongo.go` — `SaveReport`가 저장 직전 **복사본의 `Findings`를 `nil`로** 비우고 기록 (`bson:"-"` 아님 — JSON 응답에는 findings가 실려야 함)
- 수정: `internal/platform/types.go` — **죽은 필드 `Scan.Report` 제거** (F-1″, 대입 지점 0건)
- 수정: `internal/platform/taskstore/contract_test.go` — report 라운드트립 계약을 **"findings 외 필드가 라운드트립된다"**로 명확화 (findings 라운드트립은 `FindingsStore` 계약 소관)
- **근거**: 설계 §3.4. **경계 규칙**: TaskStore=report 골격 / FindingsStore=findings 본문

**Step 11 — 리포트 조립(조인) 지점 단일화 (+ **G3**)** `[x]`
- 수정: `internal/platform/orchestrator/orchestrator.go:177 GetReport` — **조립 지점은 코드로 확인된 이 한 곳** (`Store.LoadReport`를 호출하는 유일한 프로덕션 경로)
  - `TaskStore.LoadReport`(골격) + `FindingsStore.LoadFindings`(본문) → 완전한 `Report`
  - **G3 (중요)**: 조립 시 `rep.Findings`를 **무조건 `FindingsStore`의 것으로 덮어쓴다.** `taskstore.Mem`은 findings를 그대로 들고 있고 `Mongo`는 비운 채 돌려주므로, 덮어쓰지 않으면 **백엔드 조합(mem/mongo × mem/mongo)에 따라 동작이 달라진다**
- **조인 지점은 정확히 한 곳**으로 국한 (설계 §3.4 경계 규칙)
- 테스트: **4가지 백엔드 조합에서 `GetReport` 결과가 동일**함을 단언 (G3 회귀)

**Step 11B — 🔴 finalize 시 원장 evidence 소멸 (G1′=E ②) — 사본 수렴** `[x]`
- 수정: `internal/platform/orchestrator/finalize.go` — **`FindingsStore.SaveFindings`가 성공한 직후**, 원장의 `outcome.findings[].evidence`를 비운다
  - 신규 seam 메서드: `TaskStore.PurgeOutcomeEvidence(ctx, scanID) error` (Mongo: `updateMany({scanId}, {$unset: {"outcome.findings.$[].evidence": ""}})` / Mem: 해당 필드 클리어)
  - **순서가 중요**: findings 컬렉션 저장 **성공 후에만** 소멸. 실패 시 원장은 그대로 → 재시도·재개 가능
  - **소멸 실패는 스캔 결과에 영향 없음** — 로그 + `ledger_purge_errors_total` 카운터 (다음 finalize/스윕에서 재시도 가능)
  - 원장의 나머지(`id`/`failed`/`notApplicable`/상태/lease)는 **감사·재개를 위해 보존**
- **근거**: **data-census §5 G1′=E**, 규칙 **O-2**(전송 사본은 목적 종료 시 소멸). 원장의 findings는 finalize 이후 **아무도 읽지 않는 죽은 사본**이다
- **결과**: 정상 종료 후 `Evidence` 사본 = **`findings` 컬렉션 1벌**. 실패·중단 스캔은 재개를 위해 (암호화된) evidence 유지
- 테스트: finalize 성공 후 원장 raw 문서에 `evidence` 필드가 **없음** / `SaveFindings` 실패 시 원장 evidence가 **보존됨**(재개 가능성 유지) / 소멸 실패해도 스캔은 completed

---

### Phase D — 응답 캐시 (U2)

**Step 12 — `ResponseCache` 계약 정정 (Q2=A) + 계약 스위트** `[x]`
- 수정: `internal/scan/fetch/cache.go` — `Put(ctx,…) error` / `Get(ctx,…) (*domain.HAREntry, bool, error)` / `WipeScan(ctx,…) error`, `InMemory` 구현 갱신
- 수정: `internal/scan/fetch/fetch.go` — `f.cache.Get(ctx,…)` / `Put(ctx,…)` (Fetch가 이미 ctx 보유). **`Get`의 에러는 miss로 취급하고 재fetch**(fail-open, Q8=A) + **카운터 증가**
- 수정: `internal/scan/exec/exec.go` — 주입 전달 (시그니처 무변경)
- 생성: `internal/scan/fetch/cache_contract_test.go` — 생성자 테이블 계약 스위트: put/get 라운드트립 · miss · **wipe 후 잔존 0** · wipe 멱등 · ctx 취소 전파

**Step 13 — `fetch.Valkey` 캐시 (Q6=A, Q11=A)** `[x]`
- 생성: `internal/scan/fetch/cache_valkey.go`
  - `NewValkeyCache(ctx, addr, password string, kr crypto.Keyring, ttl time.Duration) (*Valkey, error)` — `Ping` fail-closed
  - 키: `chiller:cache:{scanId}:{sha256hex(url)}` (평문) · 스캔 인덱스 SET: `chiller:cache:{scanId}:keys`
  - 값: `crypto.Seal(kr, JSON(HAREntry), aad=scanId+url)` — **값 전체 암호화**
  - `Put`: 파이프라인 `SET key val EX ttl` + `SADD index key` + `EXPIRE index ttl`
  - `Get`: `GET` → `Open` → `HAREntry` (복호화 실패 = **에러**, 조용한 miss 아님)
  - `WipeScan`: `SMEMBERS index` → 파이프라인 `DEL` → `DEL index` (**`SCAN` 패턴 순회 금지** — O(키스페이스))
  - `Ping(ctx) error`
- **근거**: 설계 §4.1/§4.2. go-redis/v9 재사용(기존 의존성)

**Step 14 — `WipeScan` 배선 (Q3=A) — F-4 결함 해소** `[x]`
- 수정: `internal/platform/orchestrator/finalize.go` — `finalize`(completed) · `fail`(failed) 경로에서 `Cache.WipeScan(ctx, id)` 호출
- 수정: `internal/platform/orchestrator/orchestrator.go` (또는 stop 경로) — `Stop`(stopped) 경로에서도 호출
- **wipe 실패는 스캔 결과에 영향 없음** — 로그 + `cache_wipe_errors_total` 카운터. TTL이 백스톱
- **근거**: 설계 §4.3. **현재 FR-12/NFR-16이 미충족인 상태를 고치는 단계**

**Step 15 — Phase C/D 테스트** `[x]`
- `cache_contract_test.go`에 Valkey 생성자 추가 (integration 태그)
- `cache_pbt_test.go` — **PBT-06 상태 모델**: 무작위 `put/get/wipe` 시퀀스 → 모델 맵과 관측 일치, **wipe 이후 항상 miss**
- `fetch_test.go` 확장 — **fail-open 회귀**: 캐시가 항상 에러를 반환해도 `Fetch`는 성공하고 rate limiter 토큰을 소비한다
- **`wipe_regression_test.go` (핵심)** — 스캔이 **completed / stopped / failed 세 경로 각각**으로 끝난 뒤 캐시에 해당 scanId 키가 **0개**임을 단언 (F-4 회귀 방지)
- `report_pbt_test.go` — **dedup 후 `Finding.Key()` 유일성**(§3.5의 전제; 깨지면 FrameworkLabels 손상)

---

### Phase E — 조립 · 관측 · 배포

**Step 16 — Composition root (`cmd/chiller`) (+ **G4**, **G5**)** `[x]`
- 수정: `cmd/chiller/main.go`
  - 플래그: `--findings-store=mem|mongo` (기본 `mem`) · `--cache=mem|valkey` (기본 `mem`) · `--cache-ttl=6h` · `--findings-ttl=0`(무기한, Q7=C) · `--cache-addr`
  - **G5**: `--findings-store=mongo`는 **기존 `--mongo-uri`/`CHILLER_MONGO_URI`를 재사용**한다 (인스턴스 공유, Q9=C). **별도 URI 플래그를 만들지 않는다**
  - env: `CHILLER_VALKEY_CACHE_ADDR` · `CHILLER_VALKEY_CACHE_PASSWORD` · `CHILLER_EVIDENCE_KEY`
  - **fail-closed**: 백엔드를 켰는데 접속 문자열/암호화 키가 없으면 **기동 실패**
  - `Ping`을 `api.HealthCheck` 목록에 등록
  - `store.WithRetry(...)` 데코레이터로 감싸 주입
  - **G4 — role별 백엔드 매트릭스** (배포 설계 §1):

    | role | Valkey-cache | FindingsStore | Keyring | 비고 |
    |---|---|---|---|---|
    | `worker` | **필수**(캐시 쓰는 주체) | **필수** | **필수** | ⚠️ **구현 중 정정**: 워커도 finalize한다(`maybeFinalize` — 스캔의 마지막 항목을 비운 워커가 findings를 조립·저장). `--findings-store=mem`으로 두면 그 스캔 결과가 워커 프로세스 메모리에 쓰이고 **유실된다** |
    | `manager` | **필수** | **필수** | **필수** | 직접 fetch하지 않지만 `WipeScan`(Q3=A) 호출 주체라 캐시 핸들 필요 |
    | `all` | 필수 | 필수 | 필수 | 기본값 |

    - **FR-02/US-002의 전제**: **모든 워커가 같은 Valkey-cache 인스턴스를 가리켜야** cross-process 재사용이 성립한다. 서로 다른 캐시를 보면 재사용률이 0이 되고 대상 서버 요청이 워커 수만큼 늘어난다(결과는 정확하지만 **조용히** 비효율) → 매니페스트 단일 Service + 기동 경고 + `cacheHits` 관측
    - **결론: 모든 role이 모든 백엔드를 필요로 한다.** 최소권한(SECURITY-06)을 role별 백엔드 분리로 얻으려던 초기 가정은 **성립하지 않는다** — 워커가 finalize하기 때문. (분리하려면 finalize를 manager 전용으로 옮기는 별도 설계 변경이 필요; 이번 증분 범위 밖)
- **근거**: 설계 §7 + **G4/G5**. 기존 `--store`/`--queue` 스위치와 동일한 모양

**Step 17 — 관측 (Q12=A)** `[x]`
- 생성/수정: 카운터 9종 — `cache_hits_total` · `cache_misses_total` · `cache_errors_total` · `cache_wipes_total` / `cache_wipe_errors_total` · `findings_save_retries_total` / `findings_save_failures_total` · `findings_save_duration_ms` · **`ledger_purge_errors_total`** (Step 11B)
- 수정: `internal/platform/api/` — `/api/introspect/persistence` (신규, 기존 Bearer authz 상속) + `/healthz` deep에 Mongo(findings)·Valkey-cache `Ping` 추가
- 수정: `/ui` 대시보드 — 캐시 hit/miss/error, findings 저장 지연 표시
- **근거**: 설계 §5. 다음 증분의 OTel 익스포터가 이 카운터를 그대로 읽어간다

**Step 18 — 통합 테스트 (Q10=A)** `[x]`
- 생성: `internal/findings/store/store_mongo_test.go` (`//go:build integration`) — testcontainers Mongo, 계약 스위트 전체 통과
- 생성: `internal/scan/fetch/cache_valkey_test.go` (`//go:build integration`) — testcontainers Valkey, 계약 스위트 + **캐시 다운 시 fail-open**(컨테이너 정지 → 스캔 완주, RESILIENCY-14)
- 기존 `make test-integration`에 편입 (testcontainers가 컨테이너를 코드에서 띄우므로 Makefile 변경 최소)

**Step 19 — 배포 자산** `[x]`
- 수정: `deploy/compose/docker-compose.yml` — `valkey-cache` 서비스 (`--maxmemory 512mb --maxmemory-policy allkeys-lru`, AOF off) + 앱 env
- 수정: `deploy/k8s/` — `valkey-cache` Deployment/Service + ConfigMap(maxmemory/policy) + NetworkPolicy(내부 전용)
- 수정: `deploy/k8s/secret.example.yaml` — `CHILLER_VALKEY_CACHE_ADDR/PASSWORD`, `CHILLER_EVIDENCE_KEY` **템플릿 항목**(값 없음)
- **G7 — `reports` TTL 경고**: `--findings-ttl>0`을 켜면 findings만 만료되어 **report가 findings 없이 남는다**. 배포 노트에 "동일 TTL을 `reports` 컬렉션에도 적용하라"고 **명시 경고**
- **G4 — 워커 공유 캐시 경고**: 모든 워커가 **같은 Valkey-cache**를 가리키도록 매니페스트에 단일 Service 주소 사용 + 주석
- **근거**: 배포 설계 §4

**Step 20 — 검증 + 문서** `[x]`
- 검증: `go build ./...` · `go vet ./...` · `gofmt -l` · `go test ./...` · `go test -race ./...` · `make arch-check` · integration 태그 **컴파일** 확인 (Docker 없으면 실행은 불가 — **정직하게 보고**)
- 생성: `aidlc-docs/construction/persistence/code/business-logic-summary.md` — 구현 요약 + **정직한 미완 범위** + **G7 경고**
- 수정: `aidlc-docs/construction/infrastructure-design/infra-plan.md` — §1 #5/#6를 **완료 처리**, §7 순서에서 항목 2 제거, §8 "Already done"에 추가
- **수정: `aidlc-docs/construction/persistence/data-census.md` — 구현 후 실제 상태로 갱신** (S6/S7을 proposed→shipped, D1 사본 수를 3→1로, 소유권 표 확정). **G-NEW=A: 이 문서는 살아있는 대장이다**
- 수정: `aidlc-docs/aidlc-state.md` — persistence 증분 Code Gen 완료 기록 + **data-census 참조 + 규칙 O-1..O-4 등재**
- **수정: `CLAUDE.md` 또는 `aidlc-state.md` — 규칙 O-3(새 저장 싱크는 data-census에 먼저 등재) 명문화** (G-NEW=A)

---

## 3. 파일 변경 요약 (사전 선언)

**생성 (7)**
- `internal/crypto/envelope.go` + `envelope_test.go`
- `internal/findings/store/store_mongo.go` · `retry.go` · `contract_test.go` · `store_pbt_test.go` · `retry_test.go` · `store_mongo_test.go`(integration)
- `internal/scan/fetch/cache_valkey.go` · `cache_contract_test.go` · `cache_pbt_test.go` · `cache_valkey_test.go`(integration) · `wipe_regression_test.go`
- `aidlc-docs/construction/persistence/code/business-logic-summary.md`

**수정 (기존 파일 in-place — 복제본 금지)**
- `internal/domain/`: `finding.go` · `har.go` · `severity.go` · **`coverage.go`**(G2: ItemStatus + ItemRecord 태그) · **`ids.go`**(G2: ScopeKind) (+ 테스트)
- `internal/findings/store/store.go` · `store_test.go`
- `internal/findings/report/report.go` · `report_test.go`
- `internal/scan/fetch/cache.go` · `fetch.go` (+ 테스트)
- `internal/scan/exec/exec.go`
- `internal/platform/types.go` (죽은 `Scan.Report` 제거) · `taskstore/taskstore_mongo.go` (**G1′: 원장 evidence Seal/Open + PurgeOutcomeEvidence**) · `taskstore/taskstore.go`(Mem) · `taskstore/contract_test.go` · `internal/platform/seams.go` (**`TaskStore.PurgeOutcomeEvidence` 추가**)
- `internal/platform/orchestrator/finalize.go` · `orchestrator.go`
- `internal/platform/api/` (introspect + healthz + ui)
- `cmd/chiller/main.go`
- `deploy/compose/docker-compose.yml` · `deploy/k8s/*` · `Makefile`(필요 시)
- `aidlc-docs/construction/infrastructure-design/infra-plan.md` · `aidlc-docs/aidlc-state.md`

**신규 의존성: 0** (실사 F-10 — 암호화는 표준 `crypto/aes`, Mongo/Valkey 드라이버는 기존)

---

## 4. 위험 / 주의

| 위험 | 완화 |
|---|---|
| **스키마 파괴적 변경** (`Severity` int→string, `FrameworkLabels` []int→[]FindingKey, `Scan.report` 제거) | 프로덕션 데이터 없음 → 마이그레이션 불필요. **개발/스테이징 DB는 배포 전 drop** (배포 노트 + 요약 문서에 명시) |
| **리포트 JSON 외부 계약 변경** (`FrameworkLabels` 값 타입) | `Report.SchemaVersion` 추가로 소비자가 구분 가능하게 |
| **`internal/crypto` 레이어 위반** | domain을 import하지 않는 순수 유틸로 유지. Step 3에서 `make arch-check` 즉시 확인 |
| **integration 테스트를 이 환경에서 실행 불가** (Docker 없음) | **컴파일만 검증하고 정직하게 보고**. 실제 실행은 `production-readiness.md`의 blocking 항목 #1에 이미 등재됨 |
| **캐시 fail-open이 대상 서버 부하로** | rate limiter(SR-4 per-host 토큰 버킷)가 상한을 강제 — Step 15의 fail-open 회귀 테스트가 토큰 소비를 단언 |
| **`TaskStore` seam에 메서드 추가** (`PurgeOutcomeEvidence`, G1′=E) | PR-11("Infra 교체 시 인터페이스 불변")의 예외가 아니라 **새 도메인 요구**다. mem/Mongo 두 구현 + 계약 스위트를 함께 갱신하고, `NewMongo`에 `crypto.Keyring`을 주입(시그니처 확장) |
| **원장 evidence 소멸의 순서 위험** | `SaveFindings` **성공 후에만** 소멸. 실패 시 원장 보존 → 재시도·재개 가능. 소멸 자체가 실패해도 스캔은 completed(로그+카운터) — 테스트로 3가지 경우 모두 단언 (Step 11B) |
| **워커들이 서로 다른 Valkey-cache를 보면 FR-02가 조용히 무력화** | 재사용률 0 + 대상 서버 요청이 워커 수만큼 증가. 배포 매니페스트 단일 Service 주소 + 기동 로그 경고 (G4, Step 16/19) |

---

## 5. 총 **22단계** · 5 Phase

A(도메인 커널 4) → B(findings 저장 **5**: +Step 6B 원장 암호화) → C(리포트 경계 **4**: +Step 11B 원장 evidence 소멸) → D(캐시 4) → E(조립·관측·배포 5)

**갭 분석 반영 요약**: G1′=E(**Step 6B + 11B 신규**) · G2=A(Step 1 확대: enum 3종) · G3(Step 11 덮어쓰기+4조합 테스트) · G4(Step 16 role 매트릭스, Step 19 경고) · G5(Step 16) · G6(Step 6) · G7(Step 19/20) · G8(Step 5) · G-NEW=A(Step 20 규율 명문화)

각 Phase는 이전 Phase의 계약에 의존하므로 **순서대로 실행**한다.
