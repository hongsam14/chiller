# Infrastructure Design — persistence 증분 (findings U3 + scan U2)

**Stage**: CONSTRUCTION → Infrastructure Design
**Scope**: infra-plan §7 항목 2 — `FindingsStore → Mongo` (§1 #6) + `ResponseCache → Valkey` (§1 #5)
**Date**: 2026-07-13
**확정 답변**: Q1=A · Q2=A · Q3=A · Q4=A · Q5=A · Q6=A(C1) · Q7=C · Q8=A · Q9=A · Q10=A · Q11=A · Q12=A · **C2=B**
**원칙**: seam-swap — mem 구현 **옆에** 새 구현을 추가하고 `cmd/chiller`에서만 선택 (infra-plan §0)

---

## 0. 실사 정정 (플랜 §0 F-1의 정확한 사실)

플랜 작성 시 F-1을 "findings가 `scans` 문서에 임베드된다"고 적었으나, 코드를 더 확인한 결과 **정확한 사실은 다음과 같습니다.**
설계는 정정된 사실 위에서 이루어집니다.

| 항목 | 정정된 사실 |
|---|---|
| **F-1′** | findings는 `scans`가 아니라 **`reports` 컬렉션**에 저장된다. `taskstore_mongo.go:364` `SaveReport` → `reportDoc{_id: scanID, report: report.Report}` → `report.Findings []domain.Finding`가 **그 문서 안에 통째로, 평문으로** 들어간다. **결론은 동일**: findings는 이미 durable하게 Mongo에 있고, `FindingsStore`(mem)는 중복 사본이며, 16MB 한계는 **`reports` 문서**에 걸린다. |
| **F-1″ (신규)** | `platform.Scan` 구조체의 `Report *report.Report \`bson:"report"\`` 필드는 **아무도 설정하지 않는 죽은 필드**다 (`grep`으로 대입 지점 0건 확인). 모든 `scans` 문서에 `report: null`이 저장되고 있다. → 이번 증분에서 **제거**한다 (죽은 스키마 필드). |

나머지 F-2..F-11은 실사 그대로 유효합니다.

---

## 1. 논리 → 물리 매핑

| 논리 컴포넌트 (seam) | MVP impl (유지) | 이번 증분에 추가되는 impl | 물리 백엔드 | 선택 방법 |
|---|---|---|---|---|
| `store.FindingsStore` | `store.InMemory` | **`store.Mongo`** (`internal/findings/store/store_mongo.go`) | Mongo replica set (**기존 인스턴스 공유**, `findings` 컬렉션) | `--findings-store=mem\|mongo` (기본 `mem`) |
| `fetch.ResponseCache` | `fetch.InMemory` | **`fetch.Valkey`** (`internal/scan/fetch/cache_valkey.go`) | **Valkey-cache** (신규 **전용 인스턴스**) | `--cache=mem\|valkey` (기본 `mem`) |

배치 근거는 `construction/shared-infrastructure.md` (Q9=C): **Mongo는 인스턴스 공유·컬렉션 분리**, **Valkey는 큐/캐시 인스턴스 분리**
(정책 충돌: 큐 = `noeviction`+AOF / 캐시 = `allkeys-lru`+휘발성).

---

## 2. seam 계약 정정 (Q2=A) — 이번 증분의 인터페이스 변경

PR-11("Infra 교체 시 인터페이스 불변")은 **이미 ctx-first인 platform seam**을 전제로 만든 규칙이다. 이 두 seam은
애초에 그 전제를 만족하지 않았으므로(F-2/F-3), **일회성 계약 정정**을 수행하고 그 이후로 PR-11을 적용한다.

### 2.1 `FindingsStore` (findings U3)

```go
// AS-IS (ctx 없음 — 취소·타임아웃 전달 불가)
type FindingsStore interface {
    SaveFindings(scanID domain.ScanID, findings []domain.Finding) error
    LoadFindings(scanID domain.ScanID) ([]domain.Finding, error)
}

// TO-BE (E-6 ctx-first, error-last)
type FindingsStore interface {
    SaveFindings(ctx context.Context, scanID domain.ScanID, findings []domain.Finding) error
    LoadFindings(ctx context.Context, scanID domain.ScanID) ([]domain.Finding, error)
}
```

**순서-보존 계약 (신규, C2와 무관하게 필요)**: `LoadFindings`는 `SaveFindings`에 전달된 슬라이스와 **순서까지 동일한**
슬라이스를 반환한다. mem 구현은 이미 만족하며, Mongo 구현은 §3.2의 `idx` 필드 + `sort`로 만족시킨다.
계약 스위트와 PBT 불변식으로 못 박는다 (§6).

### 2.2 `ResponseCache` (scan U2)

```go
// AS-IS (ctx·error 모두 없음 — 네트워크 백엔드의 실패를 표현할 수 없음)
type ResponseCache interface {
    Put(scanID domain.ScanID, url string, e *domain.HAREntry) error
    Get(scanID domain.ScanID, url string) (*domain.HAREntry, bool)
    WipeScan(scanID domain.ScanID) error
}

// TO-BE
type ResponseCache interface {
    Put(ctx context.Context, scanID domain.ScanID, url string, e *domain.HAREntry) error
    Get(ctx context.Context, scanID domain.ScanID, url string) (*domain.HAREntry, bool, error)
    WipeScan(ctx context.Context, scanID domain.ScanID) error
}
```

`Get`의 3-값 반환이 **Q8(fail-open)을 관측 가능하게** 만드는 전제다 — 에러를 조용히 miss로 삼키는 대신,
호출자가 "miss"와 "백엔드 장애"를 **구분해서 계수**할 수 있다 (§5.1).

### 2.3 호출부 파급 (총 4곳 — 실사 F-7/F-8)

| 파일 | 변경 |
|---|---|
| `internal/scan/fetch/fetch.go` | `f.cache.Get(ctx, …)` / `f.cache.Put(ctx, …)` — `Fetch`가 이미 `ctx`를 받으므로 그대로 전달 |
| `internal/scan/exec/exec.go` | 주입만 전달 — 시그니처 무변경 |
| `internal/platform/orchestrator/finalize.go` | `SaveFindings(ctx, id, findings)` — 이미 `ctx` 보유 |
| `cmd/chiller/main.go` | 조립 스위치 (§7) |

---

## 3. FindingsStore → Mongo (Q1=A, C2=B, Q4=A, Q5=A, Q7=C, Q9=A)

### 3.1 저장 모델 — 전용 컬렉션, finding 당 1 문서 (Q1=A)

`findings` 컬렉션이 **findings의 정본**이 된다. `report.Report.Findings`는 **Mongo에 저장하지 않는다**:

| 무엇 | 어디에 |
|---|---|
| findings (정본) | **`findings` 컬렉션** — finding 당 1 문서, evidence 암호화 |
| report의 나머지 (Coverage, MappingRevision, FrameworkLabels) | `reports` 컬렉션 — **`findings` 필드는 빈 채로 저장** (`bson:"-"` 아님, 명시적으로 비움 → §3.4) |
| `scans.report` 죽은 필드 | **제거** (F-1″) |

이로써: 16MB 한계 해소(findings 수와 무관하게 문서가 작음) · 컬렉션 단위 최소 권한(SECURITY-06) · 암호화 대상 일원화 ·
`LoadFindings`가 처음으로 실사용된다.

### 3.2 `findings` 컬렉션 스키마

```
{
  _id:        "<scanId>\x1f<idx>",      // scanId + 슬라이스 위치 → 재저장 시 멱등(upsert)
  scanId:     "<scanId>",               // 인덱스 키
  idx:        0,                        // 원본 슬라이스 위치 — 순서 복원용 (§2.1 계약)
  key:        "<sha256hex>",            // domain.Finding.Key() — FrameworkLabels가 참조 (C2=B, §3.5)
  cwes:       ["CWE-614","CWE-1004"],   // 정규화됨 (R1)
  targetUrl:  "https://…",
  sourceTool: "observation:CWE-614",
  severity:   "medium",                 // 문자열 enum (Q4=A, §3.3)
  evidence:   {                         // 봉투 암호화 (Q5=A, §3.6)
    v:     1,
    keyId: "k1",
    nonce: BinData(...),
    ct:    BinData(...)                 // AES-256-GCM(JSON([]string snippets))
  },
  schemaVersion: 1,
  createdAt:  <int64 unix>              // TTL 인덱스의 근거 필드 (Q7=C, §3.7)
}
```

**인덱스**

| 인덱스 | 목적 |
|---|---|
| `{scanId: 1, idx: 1}` | `LoadFindings`의 조회 + 정렬을 **인덱스만으로** 처리 (covered sort) |
| `{scanId: 1, key: 1}` | 키 기반 단건 조회 (report 소비자·향후 API) |
| `{createdAt: 1}` + `expireAfterSeconds` (**선택적, 기본 비활성**) | 보존 정책 (Q7=C, §3.7) |

**쓰기**: `SaveFindings`는 **bulk `ReplaceOne(upsert)`** — `_id`가 `scanId+idx`라 **멱등**하다(재시도·재실행 시 중복 없음).
같은 스캔을 다시 저장할 때 **이전 실행이 더 많은 finding을 남겼을 수 있으므로**, upsert 후 `deleteMany({scanId, idx: {$gte: len}})`로
꼬리를 잘라낸다 (그렇지 않으면 유령 finding이 남는다). write concern은 `majority`(taskstore와 동일, RPO≈0).

**읽기**: `LoadFindings` = `find({scanId}).sort({idx: 1})` → evidence 복호화 → `[]domain.Finding`.
순서는 `idx`로 정확히 복원된다.

### 3.3 직렬화 계약 (Q4=A)

직전 증분(platform)이 채택한 정책을 **그대로 확장**한다.

- `domain.Finding` / `Evidence` / `HAREntry` / `Header` / `Cookie` 에 **명시적 `bson:` 태그** 추가.
  (지금은 Go 필드명 소문자화에 의존 — 필드 리네임이 곧 조용한 스키마 파괴)
- **`domain.Severity`를 문자열로 마샬링** — `bson.ValueMarshaler`/`ValueUnmarshaler` 구현으로
  `"info"|"low"|"medium"|"high"|"critical"`. **`Severity`는 이미 int로 Mongo에 저장되고 있다**(`reports` 컬렉션 안).
  iota 순서가 바뀌면 기존 문서가 조용히 오염되는 **잠재 결함이 이미 존재**하며, 프로덕션 데이터가 없는 **지금이 고치는 가장 싼 시점**이다.
- 모든 저장 문서에 `schemaVersion: 1`.

**마이그레이션**: 프로덕션 데이터 없음 → **마이그레이션 스크립트 불필요**. 기존 `reports` 문서는 스키마가 바뀌므로
개발 환경 DB는 **drop 후 재생성**한다 (배포 노트에 명시).

### 3.4 `Report`의 findings 비우기 — 정확한 메커니즘

`Report.Findings`에 `bson:"-"`를 다는 것은 **틀린 해법**이다. `report.Report`는 `/api/scans/{id}/report`의
**JSON 응답 본문이기도** 하므로 findings는 JSON에는 반드시 실려야 한다. 저장 계층에서만 비운다:

- `taskstore.Mongo.SaveReport`가 저장 직전 **복사본을 만들어 `Findings = nil`로 두고** 쓴다.
- `taskstore.Mongo.LoadReport`는 findings 없는 report를 돌려준다 → **재조립은 orchestrator/ScanAPI 계층**에서
  `FindingsStore.LoadFindings(ctx, id)`로 채운다 (2 쿼리).
- `taskstore.Mem`은 그대로 (findings를 그냥 들고 있음) — **동작 동일, 저장 표현만 다름**. 계약 스위트가
  "저장→로드 시 report가 findings를 포함해 동일" 을 요구하지 않도록, 계약을 **"report의 findings 외 필드가 라운드트립된다"**로
  명확히 하고 findings 라운드트립은 `FindingsStore` 계약이 책임진다.

> **경계 규칙**: `TaskStore`는 report의 **골격**(coverage/labels/revision)을, `FindingsStore`는 **findings 본문**을 소유한다.
> 두 저장소의 조인 지점은 **정확히 한 곳** — report 조립 함수 — 으로 국한한다.

### 3.5 `FrameworkLabels` → finding 키 (C2=B)

인덱스 참조를 **키 참조**로 바꿔 순서 의존성을 **구조적으로 제거**한다.

```go
// domain (신규)
type FindingKey string                       // sha256hex(IdentityKey()) — 인코딩 안전(0x1f 미포함), 안정적
func (f Finding) Key() FindingKey            // IdentityKey()를 SHA-256 해시 → hex

// report (변경)
type Report struct {
    ScanID          domain.ScanID
    Findings        []domain.Finding
    Coverage        domain.Coverage
    MappingRevision string
    FrameworkLabels map[domain.Framework]map[string][]domain.FindingKey  // ← []int 에서 변경
}
```

**왜 해시인가**: `Finding.IdentityKey()`는 구분자로 `0x1f`(unit separator)를 쓴다. 이걸 그대로 JSON/BSON 키·값에 실으면
제어문자가 인코딩을 타야 해 취약하다. SHA-256 hex는 **불투명·안정·인코딩 안전**하고, 소비자는 `Report.Findings`의
각 원소에 대해 `f.Key()`를 계산해 **스스로 해석**할 수 있다(추가 룩업 테이블 불필요).

**전제**: dedup 이후 findings의 `IdentityKey()`는 **유일**하다 (`dedup.go:25`가 바로 그 키로 collapse한다).
`report.Builder`는 dedup 결과만 받으므로 키 충돌은 발생하지 않는다 — 이 전제를 **PBT 불변식**으로 검증한다(§6).

**파급**: `report.Builder.Build`(라벨 생성) · `report_test.go` · 리포트 JSON 소비자(외부). 외부 계약 변경이므로
`schemaVersion`을 리포트에도 실어 소비자가 구분할 수 있게 한다.

### 3.6 Evidence 암호화 (Q5=A) — 앱 레벨 봉투 암호화

| 항목 | 결정 |
|---|---|
| 알고리즘 | **AES-256-GCM** (표준 `crypto/aes` + `crypto/cipher` — **신규 의존성 없음**) |
| 대상 | `Evidence.Snippets`**만** (`[]string` → JSON → 암호화). CWE/URL/tool/severity는 **평문**(쿼리·인덱스 대상) |
| 평문 포맷 | `JSON([]string)` |
| 봉투 | `{v: 1, keyId: "k1", nonce: 12B, ct: bytes}` — nonce는 **쓰기마다 `crypto/rand`로 새로 생성** |
| 키 | 32B, `CHILLER_EVIDENCE_KEY`(base64) 로 주입. K8s Secret / 볼트 (SECURITY-12). **소스·이미지 하드코딩 금지** |
| 키 회전 | 봉투의 `keyId`로 다중 키 지원 — 복호화는 `keyId`로 키를 고르고, 쓰기는 항상 **현재 키**. 구 키는 읽기 전용으로 유지 |
| 키 부재 시 | `--findings-store=mongo` 인데 키가 없으면 **기동 실패(fail-closed)**. mem 구현은 암호화하지 않음(프로세스 메모리) |
| AAD | `scanId` + `key`(finding key)를 GCM의 **Additional Authenticated Data**로 묶어, 봉투를 다른 finding으로 **옮겨 붙이는 것을 차단** |

**왜 CSFLE가 아닌가**: Mongo CSFLE는 key vault + `mongocryptd`/`crypt_shared` 바이너리 운영이 붙어 이미지·운영 부담이 크고,
우리는 **evidence를 검색할 필요가 전혀 없다**(항상 `scanId`로만 조회). 봉투 암호화로 충분하며 백엔드 독립적이다.

### 3.7 보존 정책 (Q7=C)

- **기본값: 무기한 보존** (TTL 인덱스 **비활성**).
- `--findings-ttl=<duration>` (예: `2160h` = 90d) 을 주면 **`{createdAt: 1}` TTL 인덱스를 생성**한다. 0/미지정이면 만들지 않는다.
- 운영 정책으로 위임 — 코드는 스위치만 제공.
- `reports` 컬렉션 TTL은 **이번 증분 범위 밖** (findings만 대상). 둘의 수명이 갈리면 report가 findings 없이 남을 수 있으므로,
  TTL을 켜는 운영자를 위해 **동일 TTL을 `reports`에도 적용할 것을 배포 노트에 경고**한다.

### 3.8 저장 실패 시 동작 (Q9=A)

`finalize`의 `SaveFindings` 실패 → 즉시 `fail(scan)` (현행) 이 아니라:

- **유한 재시도 + 지수 백오프**: 3회, 100ms→400ms→1.6s (jitter 포함), `ctx` 취소 시 즉시 중단.
- 재시도 대상은 **일시적 오류만** — Mongo 드라이버가 `retryable` 로 분류하는 네트워크/일시 오류. **`context.Canceled`·중복키·검증
  실패는 재시도하지 않는다**(즉시 실패).
- 소진 후에도 실패하면 **`scan = failed`** (현행 동작). 결과를 조용히 버리지 않는다.
- Mongo write concern `majority` + 드라이버의 retryable-writes 와 결합 (RESILIENCY-04).

---

## 4. ResponseCache → Valkey (Q6=A/C1, Q3=A, Q8=A, Q11=A)

### 4.1 키·값·수명

| 항목 | 결정 |
|---|---|
| 인스턴스 | **Valkey-cache** — 큐와 **별도 인스턴스** (Q11=A, shared-infrastructure §2) |
| 키 | `chiller:cache:{scanId}:{sha256hex(url)}` — URL을 **해시**해 길이·특수문자 문제를 제거. 키는 **평문** |
| 스캔 인덱스 | `chiller:cache:{scanId}:keys` (SET) — `WipeScan`이 `SCAN`(O(N) 전수 순회) 없이 정확 삭제하기 위한 멤버십 집합 |
| 값 | `AES-256-GCM(JSON(domain.HAREntry))` — **값 전체 암호화** (Q6=A). Valkey는 불투명 바이트만 본다 |
| 직렬화 | JSON (`HAREntry.Body []byte` → base64, 표준 동작). 본문은 이미 fetcher가 bound (NFR-16) |
| TTL | 모든 키에 TTL (기본 **6h**, `--cache-ttl`). 인덱스 SET에도 동일 TTL |
| eviction | 인스턴스 정책 `allkeys-lru` + `maxmemory` (배포 설정, §deployment-architecture) |

**AAD**: `scanId` + `url`을 GCM AAD로 묶어, 캐시 엔트리를 **다른 URL의 응답으로 위장 주입하는 것**을 차단
(캐시 오염 → 오탐/미탐으로 이어질 수 있는 실제 공격면).

### 4.2 암호화와 regex 탐색의 관계 (C1=A — 확인 완료)

암호화 경계는 **`ResponseCache` 구현 내부**에서 닫힌다:

```
fetcher.Fetch  ──Put(ctx,…)──▶  [직렬화 → AES-GCM 암호화]  ──▶  Valkey (불투명 바이트)
                                                                    │
observation 체커 ◀── *domain.HAREntry (평문) ◀── [복호화 → 역직렬화] ◀┘  Get(ctx,…)
                     └─ regexp.Match(entry.Body)   ← 코드 무변경
```

- `internal/scan/observation/*`의 **10개 시그니처는 한 줄도 바뀌지 않는다.** 지금 `InMemory` 캐시가 주는 것과
  **동일한 평문 `*HAREntry`** 를 받는다.
- Valkey **안에서 내용 검색을 하지 않기 때문에** 성립한다 — 조회는 정확 키 룩업뿐이고, 키는 평문이다.
- 비용: `Get`당 AES-NI 복호화 1회(µs; Valkey RTT에 묻힘) + 평문의 프로세스 메모리 상주(**fetcher가 응답을 읽는 순간부터
  이미 그러함** — 암호화가 막는 것은 Valkey 인스턴스 쪽 노출: 메모리 덤프·RDB·백업·운영자 접근).

### 4.3 `WipeScan` 배선 (Q3=A) — **현존 결함의 해소**

**현재 `WipeScan`은 아무 곳에서도 호출되지 않는다** (F-4). FR-12/NFR-16(스캔 종료 시 응답 본문 폐기)이 **지금 미충족**이며 (SECURITY-01/11),
mem 캐시라 프로세스 종료가 이를 가려 왔다. Valkey로 옮기면 **본문이 백엔드에 잔류**한다.

**이중 안전망**으로 고친다:

| 층 | 메커니즘 |
|---|---|
| 명시적 삭제 | orchestrator의 **모든 종료 경로**에서 `Cache.WipeScan(ctx, id)` 호출 — `finalize`(completed) · `fail`(failed) · `Stop`(stopped). 세 경로 모두 (실패해도 스캔 결과에는 영향 없음 — **에러는 로그+카운터**로 노출) |
| 백스톱 (TTL) | 모든 캐시 키에 TTL(§4.1) — 프로세스가 죽어 `WipeScan`이 못 돌아도 **만료로 반드시 사라진다** |

`Valkey.WipeScan` 구현: 인덱스 SET(`…:keys`)을 `SMEMBERS` → 파이프라인 `DEL` → 인덱스 SET 자체 `DEL`.
(`SCAN`+패턴 순회는 O(전체 키스페이스)라 **쓰지 않는다** — 큐와 인스턴스가 분리되어 있어도 여전히 비싸다.)

### 4.4 백엔드 장애 시 동작 (Q8=A — fail-open)

캐시는 **성능 최적화**(FR-02 단일 fetch 재사용)이지 정확성 요소가 아니다.

- `Get`이 에러를 반환하면 **fetcher는 miss로 취급하고 재fetch**한다 → 캐시가 죽어도 스캔은 (느리게) 완주.
- 재fetch는 **여전히 rate limiter(SR-4 per-host 토큰 버킷)와 approval gate를 통과**한다 → 대상 서버 폭주 없음.
- `Put` 실패도 무시(로그+카운터) — 캐시에 못 넣었을 뿐, 결과는 정확하다.
- **조용히 삼키지 않는다**: `cache_errors_total` 카운터 + 로그. `Get`의 3-값 반환(§2.2)이 "miss"와 "장애"를 구분해 계수할 수 있게 한다.
- **fail-open의 유일한 위험**은 대상 서버 부하 증가인데, rate limiter가 이미 그 상한을 강제한다 → 수용 가능.

---

## 5. 관측 (Q12=A)

OTel은 infra-plan #5(다음 증분)다. 이번 증분은 **기존 창구를 확장**하고, 다음 증분의 OTel 익스포터가 **그대로 읽어갈 수 있는 카운터**를 남긴다.

### 5.1 카운터 (프로세스 내, `expvar`/구조체 — OTel이 나중에 이걸 읽음)

| 지표 | 의미 |
|---|---|
| `cache_hits_total` / `cache_misses_total` | FR-02 재사용률 — **캐시가 실제로 일하고 있는지**의 유일한 증거 |
| `cache_errors_total` | 백엔드 장애 (fail-open이 **조용하지 않도록**, Q8) |
| `cache_wipes_total` / `cache_wipe_errors_total` | FR-12 폐기가 실제로 도는지 |
| `findings_save_retries_total` / `findings_save_failures_total` | Q9 재시도 가시화 |
| `findings_save_duration_ms` (마지막/최대) | Mongo 지연 |

### 5.2 노출

- **`/healthz` (deep)**: Mongo(findings) `Ping` + Valkey-cache `Ping` 추가. 실패 시 degraded (SECURITY-04 인증 적용 — 기존 규칙 유지).
- **`/api/introspect/persistence`** (신규): 위 카운터 JSON.
- **`/ui` 대시보드**: 캐시 hit/miss/error, findings 저장 지연 표시.

---

## 6. 테스트 전략 (Q10=A — PBT 확장 FULL, blocking)

| 층 | 내용 |
|---|---|
| **계약 스위트** | `store`·`fetch` 각각에 **생성자 테이블** 계약 스위트를 추가(현재 두 패키지는 구상 생성자에 하드와이어된 개별 테스트뿐 — platform이 직전 증분에 한 것과 같은 추출). mem/Mongo/Valkey가 **같은 스위트**를 통과한다. 필수 케이스: 라운드트립, **순서 보존**(§2.1), 없는 스캔 = 빈 결과, `WipeScan` 후 잔존 0, 재저장 멱등, 꼬리 삭제(§3.2) |
| **PBT** | ① `SaveFindings→LoadFindings` 라운드트립 항등(**순서·CWE 정규화·`MaxEvidenceSnippets` 경계 보존**) ② 암복호 항등 `Dec(Enc(x)) == x` + **AAD 불일치 시 반드시 실패**(봉투 이식 차단) ③ 캐시 상태 모델(put/get/wipe 시퀀스 → 모델과 실제 일치, wipe 후 항상 miss) ④ **dedup 후 `Finding.Key()` 유일성**(§3.5 전제) |
| **통합** | `//go:build integration` + testcontainers (Mongo, Valkey) — 직전 증분 골격 그대로. `make test-integration`에 편입 |
| **회귀** | F-4(WipeScan 미배선)에 대한 **명시적 회귀 테스트**: 스캔이 completed/stopped/failed 각 경로로 끝난 뒤 캐시에 해당 scanId 키가 **0개**임을 단언 |

---

## 7. Composition root (`cmd/chiller`)

기존 `--store`/`--queue` 스위치와 **같은 모양**으로 2개 추가:

| 플래그 | 값 | 기본 | 필요 설정 |
|---|---|---|---|
| `--findings-store` | `mem` \| `mongo` | `mem` | `mongo`: 기존 `--mongo-uri`/`CHILLER_MONGO_URI` **재사용**(인스턴스 공유) + `CHILLER_EVIDENCE_KEY` 필수 |
| `--cache` | `mem` \| `valkey` | `mem` | `valkey`: `--cache-addr`/`CHILLER_VALKEY_CACHE_ADDR` + `CHILLER_VALKEY_CACHE_PASSWORD` + `CHILLER_EVIDENCE_KEY` |
| `--cache-ttl` | duration | `6h` | — |
| `--findings-ttl` | duration | `0` (무기한, Q7=C) | `>0`이면 TTL 인덱스 생성 |

**fail-closed**: 백엔드를 선택했는데 접속 문자열이나 암호화 키가 없으면 **기동 실패**(직전 증분의 `--store=mongo` 가드와 동일 규율).
`Ping`을 헬스체크 목록에 등록한다.

---

## 8. 데이터 계약 요약

| 계약 | 값 |
|---|---|
| Mongo DB | `chiller` (공유) |
| 컬렉션 (신규) | `findings` |
| 컬렉션 (변경) | `reports` — `report.findings` 를 **비워서** 저장 / `scans` — 죽은 `report` 필드 **제거** |
| 인덱스 | `findings`: `{scanId:1, idx:1}`, `{scanId:1, key:1}`, (선택) `{createdAt:1}` TTL |
| Valkey-cache 키 | `chiller:cache:{scanId}:{sha256hex(url)}` (값 암호화, TTL) · `chiller:cache:{scanId}:keys` (SET, TTL) |
| enum 표현 | `Severity` = 문자열 (`"info"…"critical"`) — **int에서 변경** |
| 스키마 버전 | 모든 신규/변경 문서에 `schemaVersion: 1` |
| 암호화 봉투 | `{v:1, keyId, nonce(12B), ct}` — AES-256-GCM, AAD = (scanId, key\|url) |
| 마이그레이션 | **불필요** (프로덕션 데이터 없음). 개발 DB는 drop 후 재생성 |

---

## 9. 이번 증분이 해소하는 기존 결함 (요약)

이 증분의 실제 가치는 "새 백엔드 추가"보다 **이미 존재하는 결함 3건의 해소**에 있다:

1. **FR-12/NFR-16 미충족** — `WipeScan`이 호출되지 않아 응답 본문 폐기가 **동작하지 않고 있었다** (F-4 → §4.3).
2. **evidence 평문 저장** — `reports` 컬렉션에 응답 조각이 **평문**으로 durable 기록되고 있었다 (F-1′ → §3.6).
3. **`Severity` int iota 오염 리스크** — 상수 순서 변경이 기존 문서를 조용히 오염시키는 **잠재 결함이 이미 존재** (F-6 → §3.3).

추가로 **죽은 스키마 필드**(`scans.report`, F-1″)와 **16MB 문서 한계**(§3.1)를 함께 정리한다.
