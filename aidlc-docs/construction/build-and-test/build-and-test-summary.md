# Build and Test Summary — chiller

**Date**: 2026-07-13 (**r3 — persistence 증분 + 코드 리뷰 하드닝** folded in).
Prior: r2 2026-07-11 (platform infrastructure increment) · r1 2026-07-10 (MVP, 4 units).
**Scope of r3**: `FindingsStore`→Mongo · `ResponseCache`→Valkey · `internal/crypto` 봉투 암호화 · 원장 evidence 봉인+소멸 ·
enum 제로값 sentinel · `FrameworkLabels`→`FindingKey` · workflow 코드 리뷰(high)에서 나온 9건 수정.

## Build Status

| 검증 | 명령 | 결과 |
|---|---|---|
| 빌드 | `go build ./...` | ✅ |
| 정적 분석 | `go vet ./...` | ✅ |
| 정적 분석 (통합 태그) | `go vet -tags integration ./...` | ✅ |
| 포맷 | `gofmt -l internal cmd` | ✅ 0 files |
| 아키텍처 가드 | `make arch-check` | ✅ OK (data plane이 platform을 import하지 않음 · domain이 어떤 unit도 import하지 않음) |

- **Build Artifacts**: `chiller` — 단일 바이너리, 4 role (`all|manager|worker|proxy`); `Dockerfile` (distroless nonroot).
- **Direct dependencies**: `yaml.v3` · `playwright-go v0.5200.1` · `retryablehttp-go v1.3.17` · `mongo-driver v1.17.9` ·
  `go-redis/v9 v9.20.1` · `testcontainers-go v0.37.0` (test-only). **r3의 신규 의존성: 0건** — 암호화는 Go 표준
  `crypto/aes`+`crypto/cipher`로 구현했다. 공급망 표면 불변 (SECURITY-10).

---

## Test Execution Summary

### Unit Tests (`go test ./...`, 외부 서비스 없음)

| 항목 | 값 |
|---|---|
| 패키지 (유닛) | **23 ok / 0 FAIL** |
| 테스트 (유닛) | **197 PASS** |
| **테스트 (통합 포함)** | **239 PASS / 0 FAIL** |
| Race detector | ✅ `go test -race ./...` clean |
| 커버리지 (유닛만) | 49.9% |
| **커버리지 (통합 포함)** | **63.8%** |

유닛만 돌리면 커버리지가 낮게 나오는 것은 **회귀가 아니라 durable 백엔드가 `integration` 태그 뒤에 있기 때문**이다.
통합 테스트를 함께 돌리면 **63.8%**로 회복된다 (r1 66.0%와 동등 수준).

---

## Integration Tests — ✅ **실제 컨테이너에서 실행됨 (22/22 ok, FAIL 0)**

> **정정**: r3 초안은 "이 환경에 Docker가 없어 실행 불가"라고 적었다. **틀렸다** — Docker가 있었고,
> 확인하지 않고 단정했다. 실제로 돌렸더니 **결함 3건이 나왔다** (아래 §"통합 테스트가 잡아낸 것").

**실행**: `make test-integration` (= `go test -tags integration -timeout 30m ./...`).
testcontainers가 Mongo/Valkey 컨테이너를 코드에서 직접 띄운다 — 사전 `docker-compose up` 불요.

| 스위트 | 파일 | 결과 |
|---|---|---|
| TaskStore 계약 (Mongo) + **원장 evidence at-rest 암호화** | `platform/taskstore/taskstore_mongo_test.go` | ✅ **PASS** (6.4s) |
| **WorkQueue 계약 (Valkey)** | `platform/workqueue/workqueue_valkey_test.go` | ✅ **PASS** (3.3s) — **버그 수정 후** |
| **FindingsStore 계약 (Mongo)** + evidence at-rest 암호화 + TTL 인덱스 | `findings/store/store_mongo_test.go` | ✅ **PASS** (6.1s) |
| **ResponseCache 계약 (Valkey)** + 값 at-rest 암호화 + TTL + wipe | `scan/fetch/cache_valkey_test.go` | ✅ **PASS** (4.2s) |

at-rest 암호화 테스트는 **raw BSON / raw Valkey 바이트를 직접 읽어 평문(응답 본문·세션 쿠키·evidence)이 없음을 단언**한다 —
라운드트립만 보는 테스트는 암호화 여부를 **증명하지 못한다**(평문 저장도 통과한다). **이제 실제로 검증됐다.**

---

## 🔴 통합 테스트가 잡아낸 것 — "컴파일만 검증"의 대가

세 건 모두 **통합 테스트를 실행하지 않았기 때문에** 두 번의 Build&Test가 green으로 통과하는 동안 숨어 있었다.

| # | 결함 | 영향 |
|---|---|---|
| 1 | **`make test-integration`이 `./internal/platform/...`만 돌렸다** | 이번 증분이 추가한 durable `FindingsStore`(findings 유닛)·`ResponseCache`(scan 유닛)는 **CI에서도 영원히 실행되지 않았을 것**이다. 타깃은 green인데 정작 새 백엔드는 건드리지도 않는 상태. → `./...`로 수정 |
| 2 | **🔴 Valkey `WorkQueue`의 `Drain`이 동작하지 않았다** (직전 증분부터 존재, HEAD에서도 재현) | drained 체크가 리더가 채널에 넣을 때만 일어나는데 리더는 **미리 읽어 담는다(read-ahead)** → Drain 전에 버퍼링된 항목은 **drain을 우회해 배달**된다. mem은 `Dequeue` 시점에 체크하므로 **두 백엔드의 동작이 달랐다**(계약 위반). **`StopScan`이 "중단"을 의미하지 않게 된다.** → `Dequeue`에서 drained 재확인 후 스킵 |
| 3 | **계약 스위트가 `Enqueue` 에러를 버리고 있었다** (`_ =`) + `Consume: true` 누락 | 인코딩 실패가 조용히 무시되고 Dequeue가 **25분 타임아웃까지 행(hang)** — 1초 만에 실패했어야 할 것이. → 헬퍼가 에러를 강제 검사하도록 수정 |

**교훈**: **"컴파일만 검증"은 검증이 아니다.** durable WorkQueue는 두 증분에 걸쳐 **한 번도 동작한 적이 없는데**
Build&Test는 두 번 green이었다.

---

## r3에서 추가된 테스트

### 계약 스위트 (생성자 테이블 — mem/durable이 동일 스위트를 통과해야 함)
- `findings/store/contract_test.go` — 라운드트립 · **순서 보존** · 멱등 · **꼬리 삭제** · **evidence 삭제 금지** · 앨리어싱 격리
- `scan/fetch/cache_contract_test.go` — 라운드트립 · miss · **wipe 후 잔존 0** · wipe 멱등 · 스캔 격리

### PBT (property-based-testing = FULL, blocking)
- 봉투 **암복호 항등** · **AAD 불일치 시 반드시 실패**(봉투 이식 차단) · **변조 검출**(AEAD 무결성) · **nonce 유일성**(1000회)
- findings 저장 라운드트립 불변식 (순서 · CWE 정규화 · `MaxEvidenceSnippets` 경계)
- 캐시 **상태 모델** (무작위 put/get/wipe 400스텝 → 모델과 관측 일치, wipe 후 항상 miss)

### 회귀 테스트 — 각각이 **실제로 존재했던 결함**을 재현한다

| 테스트 | 막는 결함 |
|---|---|
| `TestFinalize_WipesCache` · `TestFail_WipesCache` · `TestStop_WipesCache` | **FR-12가 작동하지 않았다** — `WipeScan`이 어디에서도 호출되지 않던 문제. 3개 종료 경로 각각 |
| **`TestFinalize_SecondFinalizerDoesNotDestroyEvidence`** | **데이터 파괴** — 2번째 finalizer가 봉인된 evidence를 빈 값으로 덮어쓰던 문제. 오케스트레이터 2개 + 저장소 1개 = 2-pod 토폴로지 재현 |
| `TestFinalize_OnlyTheCASWinnerPurges` | purge는 단일-finalizer CAS 승자만 수행 |
| `TestFinalize_DoesNotPurgeWhenTheSaveFailed` | 저장 실패 시 원장이 유일한 사본 → 보존(재개 가능) |
| `SaveNeverErasesEvidence` (계약) | evidence 없는 저장이 기존 evidence를 지우지 못함 |
| `TestItemStatus_MissingFieldIsAnError` | **컴플라이언스 미탐** — 필드 누락 문서가 "검사 완료"로 읽히던 문제 |
| `TestMarshal_RefusesInvalidEnums` | 무효 enum이 `"unknown"`으로 저장되어 그 커서의 모든 finding을 영구히 못 읽게 만들던 문제 |
| `TestScopeKind_UnsetIsNotAScope` | scope 누락 → host 체커 PS 항목이 미실행인데 checked로 기록되던 문제 |
| `TestNewKeyring_MultiKeyRequiresExplicitIDs` | 키 회전이 기존 봉투를 **틀린 키로 열던** 문제 |
| `TestFetcher_CacheFailsOpen` · `...StillChargesRateBudget` | 캐시 장애가 스캔을 죽이지 않되 **rate budget 우회로가 되지 않음**(SR-4) |
| `TestFetcher_DoesNotCacheAfterTheScanWasCancelled` | stop 후 in-flight fetch가 wipe된 캐시를 다시 채우던 문제 |
| `TestNewMongo_RejectsSubSecondTTL` | 1초 미만 TTL이 `expireAfterSeconds:0`(즉시 삭제)으로 잘리던 문제 |
| `TestGetReport_AlwaysTakesFindingsFromTheFindingsStore` | 백엔드 조합에 따라 리포트 내용이 달라지던 문제 |

---

## Security Tests

| 항목 | 결과 |
|---|---|
| `make vuln` (govulncheck) | ⚠️ **GO-2026-5856** 1건 — `crypto/tls` ECH. **Go 표준 라이브러리** 문제이며 `crypto/tls@go1.26.4`에서 발견, **go1.26.5에서 수정**. **소스 변경 불필요 — 툴체인 업그레이드만 하면 된다.** r2에서 이미 식별된 것과 **동일**하며, **r3가 새로 만든 취약점은 없다** |
| 신규 의존성 | **0건** — 공급망 표면 불변 (SECURITY-10) |
| at-rest 암호화 | AES-256-GCM 봉투 (evidence + 캐시 값 + 원장 evidence). AAD 바인딩으로 봉투 이식·캐시 오염 차단 |
| fail-closed | durable 백엔드를 켰는데 접속 문자열이나 `CHILLER_EVIDENCE_KEY`가 없으면 **기동 거부** (SECURITY-15) |

## Performance Tests

**미실행 (N/A).** p95 SLO는 fleet 확정 후 Operations에서 perf 곡선법으로 측정하기로 이미 결정됨(Q12=B).
r3의 성능 관련 변경은 둘 다 관측 카운터를 남겼다: 캐시 `Get`마다 AES-GCM 복호화 1회(AES-NI, Valkey RTT에 묻힘) ·
findings 저장이 finalize에서 bulk 1회(`findingsSaveLastMs`).

---

## Overall Status

| | |
|---|---|
| Build | ✅ Success |
| Unit Tests | ✅ 197 PASS / 0 FAIL |
| **Integration Tests** | ✅ **22/22 패키지 ok / 0 FAIL** (실 Mongo/Valkey 컨테이너). 통합 포함 총 **239 PASS** |
| Race | ✅ clean |
| Architecture guard | ✅ OK |
| Security scan | ⚠️ 툴체인 취약점 1건 (Go 1.26.5로 해소, 소스 무관) |
| **Ready for Operations** | **Yes** — durable 백엔드가 실제로 검증됐다 |

### 프로덕션 전 blocking
1. ~~`make test-integration` 실행~~ — ✅ **해소 (2026-07-13)**. 22/22 ok. CI 파이프라인에 편입할 것 (타깃이 이제 `./...`)
2. **Go ≥ 1.26.5 툴체인으로 재빌드** (GO-2026-5856)
3. 이미지 태그 → 다이제스트 고정 · 시크릿 out-of-band · Mongo/Valkey TLS+인증 · Mongo replica set
4. **(r3 신규) 롤링 업그레이드 전 큐 드레인** — `ScopeKind` 인코딩이 int→이름으로 바뀌어, 업그레이드 시점에
   Valkey 큐에 남아 있던 `WorkItem`을 새 빌드가 디코딩하지 못한다. 실행 중인 스캔이 없을 때 배포하거나 큐를 비운 뒤 배포할 것
5. **(r3 신규) 개발/스테이징 Mongo DB drop** — enum 이름 저장 · `FrameworkLabels` 타입 변경 · `scans.report` 제거로
   기존 문서와 비호환. 프로덕션 데이터가 없어 마이그레이션 스크립트는 불필요
