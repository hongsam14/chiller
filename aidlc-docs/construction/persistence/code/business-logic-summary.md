# Code Generation Summary — persistence 증분 (findings U3 + scan U2)

**Date**: 2026-07-13
**Scope**: infra-plan §7 항목 2 — `FindingsStore → Mongo` (#6) + `ResponseCache → Valkey` (#5)
**Verified**: `go build` · `go vet` · `gofmt -l` clean · `go test ./...` pass · `go test -race` clean · `make arch-check` OK
**신규 의존성: 0** (암호화는 표준 `crypto/aes`+`crypto/cipher`; Mongo/Valkey 드라이버는 기존)

---

## 1. 이 증분이 실제로 한 일

새 백엔드 2개를 붙인 것보다, **이미 존재하던 결함 3건을 닫은 것**이 본질이다.

| # | 결함 (증분 전 상태) | 해소 |
|---|---|---|
| ① | **FR-12/NFR-16 미작동** — `WipeScan`을 **아무도 호출하지 않았다**. 스캔이 끝나도 응답 본문이 폐기되지 않았고, in-memory 캐시라 프로세스 종료가 이를 가려 왔다 | orchestrator의 **세 종료 경로 전부**(completed/finalize · failed/fail · stopped/StopScan)에서 `WipeScan` 호출 + 모든 캐시 키에 **TTL 백스톱**(프로세스가 죽어 wipe를 못 해도 만료로 사라짐) |
| ② | **evidence 평문 durable 저장** — findings가 `reports` 컬렉션에, 그리고 **`workledger`에도** 평문으로 저장되고 있었다 | 두 곳 **모두** AES-256-GCM 봉투 암호화 + finalize 시 원장 사본 **소멸** (아래 §3) |
| ③ | **int-iota enum 오염 위험** — `Severity`·`ItemStatus`·`ScopeKind`가 ordinal로 저장 중 | 세 enum 전부 **이름으로 저장** + **제로값을 무효화** (아래 §2) |

부수적으로 죽은 스키마 필드(`platform.Scan.Report` — 대입 지점 0건, 모든 문서에 `report: null`)를 제거했고,
`reports` 문서의 16MB BSON 한계를 findings 분리로 해소했다.

---

## 2. 제로값이 진짜 문제였다 (G2 — 구현 중 발견)

설계는 "enum을 이름으로 저장하면 ordinal 오염이 사라진다"고 판단했다. **테스트를 쓰면서 그것만으로는 부족함이 드러났다.**

BSON 디코더는 **문서에 없는 필드에 대해 커스텀 언마샬러를 호출하지 않는다.** Go 제로값이 그대로 남는다.
`StatusChecked`가 iota 0이었으므로 — **status 필드가 없는 coverage 문서는 "chiller가 검사했고 판정했다"로 읽힌다.**
저장 결함이 **컴플라이언스 미탐**으로 둔갑한다. 이 시스템이 틀릴 수 있는 방식 중 최악이다.

**해법: 제로값 자체를 무효로 만든다.**
- `StatusUnset ItemStatus = iota` (0번, 절대 유효하지 않음) → 필드 누락 문서는 `Coverage.Validate`(R5)에서 **실패**한다
- `SevUnset Severity = iota` (같은 이유; severity 필드가 사라진 finding이 조용히 "info"로 강등되는 것을 막는다) → `Finding.Validate`(R4)에서 실패
- `ScopeUnset ScopeKind = iota` — **코드 리뷰 후 추가**. 처음엔 "라우팅 분기라 범위 밖"으로 미뤘으나, 리뷰가 **같은 미탐 부류**임을 확인했다: scope 필드가 없으면 URLScoped로 읽혀 host 체커(testssl/dirhunt/subzy)의 PS 항목이 **실행되지 않았는데 checked로 기록**된다. 두 분기점(`exec.Run`, `checkersForScope`)이 이제 unset을 **거부**한다(항목을 tool-failed로 — 리포트에 보이는 실패가 안 보이는 오분류보다 낫다).
- **write 측도 막는다** (코드 리뷰 후 추가): 마샬러가 무효 값에 대해 `"unknown"`을 쓰는 대신 **에러를 반환**한다. `"unknown"`은 파싱되지 않으므로, 저장되면 그 문서가 영구히 읽히지 않고 **같은 커서의 다른 finding까지 전부** 못 읽게 만든다.

테스트 `TestItemStatus_MissingFieldIsAnError` · `TestMarshal_RefusesInvalidEnums` · `TestScopeKind_UnsetIsNotAScope`가 각각을 단언한다.

---

## 3. evidence 사본이 3벌 → 1벌 (G1′=E)

전수조사(`data-census.md`)에서 드러난 구조: **원장이 findings의 전송 경로다.**
worker가 `RecordOutcome`으로 원장에 쓰고 → manager/worker의 `finalize`가 `AllOutcomes`로 읽어 → dedup → findings 컬렉션.
따라서 evidence는 **반드시 원장을 거친다.**

```
                                   [증분 전]                      [증분 후]
workledger.outcome.findings   평문, durable, 영구            봉인(AES-GCM) → finalize 시 소멸
reports.report.findings       평문, durable, 영구            저장 안 함 (findings 컬렉션이 소유)
findings 컬렉션                (없음)                         봉인(AES-GCM), 정본
```

- **`taskstore.Mongo`**: `RecordOutcome`이 `Seal`, `AllOutcomes`가 `Open` (AAD = 작업항목 id + finding key)
- **`TaskStore.PurgeOutcomeEvidence`** (신규 seam 메서드): 원장의 evidence만 `$unset`.
  나머지(id/failed/notApplicable/상태/lease)는 감사·재개를 위해 보존
- **순서가 규칙이다 (코드 리뷰 후 정정)**: 처음엔 `SaveFindings` 성공 직후 purge했는데 **그게 데이터 파괴 버그였다.**
  단일 finalizer를 보증하는 terminal CAS가 purge **뒤에** 있었고, 워커가 finalize하며 replicas=2다.
  A가 저장→purge하면 **B가 원장을 읽어 evidence 없는 findings를 얻고 재저장해 봉인된 evidence를 덮어썼다.**
  → **CAS 승자만 purge**하도록 순서를 바꿨고(`TestFinalize_OnlyTheCASWinnerPurges`),
  **저장소 계약에 "evidence를 지우지 않는다"를 추가**했다 — evidence 없는 저장이 기존 evidence를 절대 덮어쓰지 못한다
  (`SaveNeverErasesEvidence` 계약 테스트 + `TestFinalize_SecondFinalizerDoesNotDestroyEvidence` 2-프로세스 회귀).
  저장이 실패하면 원장이 유일한 사본이므로 **그대로 둔다** (`TestFinalize_DoesNotPurgeWhenTheSaveFailed`)
- 소멸 실패는 스캔에 영향 없음 — `ledgerPurgeErrors` 카운터 + 로그

**정상 종료 후 evidence 사본 = `findings` 컬렉션 1벌.** 실패·중단 스캔의 원장에는 봉인된 evidence가 남지만, **재개에 필요하다.**

---

## 4. 암호화 (`internal/crypto`, 신규)

AES-256-GCM 봉투. **AEAD이므로 복호화가 곧 무결성 검증**(변조된 값은 복호화 실패, SECURITY-13).

- `Envelope{V, KeyID, Nonce, CT}` · nonce는 **쓰기마다 새로** (`TestSeal_FreshNonceEachTime`)
- **키 회전**: `CHILLER_EVIDENCE_KEY="k2:<new>,k1:<old>"` — 첫 항목이 현재 키(새 값 봉인), 나머지는 기존 값 개봉용.
  **키가 2개 이상이면 각 항목에 id가 필수**다(코드 리뷰 후 수정): 처음엔 위치 기반으로 id를 붙였는데,
  그러면 키를 앞에 추가할 때마다 **모든 키의 이름이 조용히 바뀌어** `k1`로 봉인된 봉투가 **틀린 키로 열린다**.
  단일 무명 키는 `k1`이다 (`TestNewKeyring_MultiKeyRequiresExplicitIDs`)
- **AAD 바인딩**이 핵심: 봉투를 **다른 곳으로 옮겨 붙일 수 없다**
  - `EvidenceAAD(scanID, findingKey)` — findings 컬렉션
  - `LedgerEvidenceAAD(itemID, findingKey)` — 원장 (item id가 `<scanID>#<n>`이라 스캔도 함께 묶인다)
  - `CacheAAD(scanID, url)` — 캐시. **캐시 오염 차단**: 다른 URL의 응답으로 위장 주입하면 복호화가 실패한다
- **키 없이 durable 백엔드를 켜면 기동 실패** (fail-closed)

### 암호화와 regex 탐색 (C1)

암호화 경계는 **구현 안**에서 닫힌다. `Get`이 복호화해 **평문 `*domain.HAREntry`를 돌려주므로**
`internal/scan/observation`의 10개 시그니처는 **한 줄도 바뀌지 않았다**. Valkey는 불투명 바이트만 본다.
(통합 테스트 `TestValkey_CachedResponseIsEncryptedAtRest`가 Valkey의 raw 바이트에 본문·세션 쿠키가 없음을 단언)

---

## 5. 리포트 계약 변경 (C2=B) — 외부 소비자 영향

```go
// AS-IS
FrameworkLabels map[Framework]map[string][]int          // ← Findings 슬라이스의 인덱스
// TO-BE
FrameworkLabels map[Framework]map[string][]FindingKey   // ← sha256hex(IdentityKey())
SchemaVersion   int                                      // = 2
```

**왜**: findings를 별도 컬렉션에서 재구성하는데 Mongo는 문서 순서를 보장하지 않는다.
인덱스 참조는 "저장소가 저장 순서대로 돌려준다"에 리포트의 정확성을 걸어 두는 것이고,
백엔드를 바꾸는 순간 **엉뚱한 finding에 컴플라이언스 라벨이 붙는다**(조용한 오염).
키 참조는 순서 의존성을 **구조적으로** 없앤다. (해시를 쓴 이유: `IdentityKey()`의 구분자가 `0x1f` 제어문자라 인코딩에 싣기 취약)

`FindingsStore`는 그와 별개로 **순서 보존을 계약으로 명시**했고(Mongo는 `idx` 필드 + `sort`), 계약 스위트가 강제한다.

---

## 6. report 조립 지점 = 정확히 한 곳 (G3)

`TaskStore`는 report **골격**(coverage/labels/revision)을, `FindingsStore`는 **findings 본문**을 소유한다.
조인은 `Orchestrator.GetReport` **한 곳**뿐이고, 거기서 `rep.Findings`를 **무조건 `FindingsStore` 것으로 덮어쓴다.**

덮어쓰지 않으면: mem TaskStore는 findings를 들고 있고 Mongo는 비운 채 돌려주므로 **백엔드 조합에 따라 응답이 달라진다.**
`TestGetReport_AlwaysTakesFindingsFromTheFindingsStore`가 두 저장소에 **서로 다른** findings를 넣고 소유자의 것이 이기는지 확인한다.

---

## 7. 캐시 fail-open (Q8=A)

캐시는 최적화지 정확성 요소가 아니다. 백엔드가 죽으면 **miss로 처리하고 재fetch** → 스캔은 (느리게) 완주.

- 재fetch도 **동일한 approval gate + per-host rate budget**을 통과한다 → 대상 서버 폭주 없음
  (`TestFetcher_CacheFailureStillChargesRateBudget`가 이를 단언 — fail-open이 SR-4 우회로가 되지 않는다)
- **조용히 실패하지 않는다**: `Get`이 `(entry, bool, error)` 3-값을 반환해 호출자가 "miss"와 "장애"를 **구분해 계수**한다.
  이게 없으면 **영구 고장난 캐시가 그냥 차가운 캐시처럼 보인다** — 스캔은 성공하고, 재사용만 조용히 사라진다.

---

## 8. 관측 (`GET /api/introspect/persistence`)

| 지표 | 이게 없으면 안 보이는 것 |
|---|---|
| `cacheHits` / `cacheMisses` | FR-02 재사용이 실제로 일어나는지. **all-miss = 워커들이 서로 다른 캐시를 보고 있다**(가장 흔한 오배포) |
| `cacheErrors` | fail-open 뒤에 숨은 죽은 캐시 |
| `cacheWipes` / `cacheWipeErrors` | FR-12 폐기가 도는지 |
| `ledgerPurgeErrors` | 원장 evidence 소멸 실패 (사본이 계속 쌓임) |
| `findingsSaveRetries` / `Failures` / `LastMs` | Mongo 저장 재시도·지연 |

`/healthz`(deep)에 `mongo-findings`·`valkey-cache` Ping 추가. 다음 증분의 OTel 익스포터가 **같은 카운터를 그대로 읽어간다.**

---

## 9. 배선 (`cmd/chiller`)

| 플래그 | 기본 | 비고 |
|---|---|---|
| `--findings-store=mem\|mongo` | `mem` | mongo는 **기존 `--mongo-uri` 재사용**(인스턴스 공유, 컬렉션 분리) |
| `--cache=mem\|valkey` | `mem` | |
| `--cache-addr` | — | **큐와 같은 주소면 기동 거부** (캐시 LRU가 큐 항목을 evict한다) |
| `--cache-ttl` | `6h` | wipe 실패의 백스톱 |
| `--findings-ttl` | `0` (무기한) | Q7=C. **1분 미만 거부**(초 단위로 잘려 `expireAfterSeconds:0` = 즉시 삭제가 된다). TTL 변경 시 인덱스를 **drop 후 재생성**해 `IndexOptionsConflict`로 인한 crash-loop를 막는다; 0으로 되돌리면 인덱스를 제거한다 |

**⚠️ role별 백엔드 — 구현 중 정정**: 초기 계획은 "worker는 findings store 불요"였으나 **틀렸다.**
`maybeFinalize`가 워커에서 돈다 — 스캔의 마지막 항목을 비운 워커가 findings를 조립·저장한다.
`--findings-store=mem`인 워커는 **그 스캔의 결과를 자기 메모리에 쓰고 잃어버린다.**
**모든 role이 모든 백엔드를 필요로 한다.** (finalize를 manager 전용으로 옮기면 최소권한 분리가 가능하지만, 별도 설계 변경 — 범위 밖)

---

## 10. 정직한 미완 범위

| 항목 | 상태 |
|---|---|
| **통합 테스트가 실행되지 않았다** | `-tags integration`으로 **컴파일·vet만 검증**했다. 이 환경에 **Docker가 없다.** Mongo/Valkey 계약 스위트, at-rest 암호화 단언, TTL·wipe 검증은 **아직 실제 컨테이너에서 돌아간 적이 없다.** `production-readiness.md`의 blocking 항목 #1 그대로 — CI에서 `make test-integration`을 돌려야 검증된 것이다 |
| **스키마 파괴적 변경** | `Severity`/`ItemStatus`/`ScopeKind` 이름 저장 + 제로값 sentinel, `FrameworkLabels` 타입 변경, `scans.report` 제거 → **기존 Mongo 문서와 비호환**. 프로덕션 데이터가 없어 마이그레이션은 불필요하지만 **개발/스테이징 DB는 drop 후 재생성**해야 한다 |
| **⚠️ 롤링 업그레이드 시 큐를 먼저 비울 것** | `domain.WorkItem`은 JSON으로 Valkey 스트림에 실린다. `ScopeKind` 인코딩이 int → 이름으로 바뀌므로, **업그레이드 전에 큐에 남아 있던 WorkItem은 새 빌드가 디코딩하지 못한다**(워커가 dequeue에서 에러). 실행 중인 스캔이 없는 상태에서 배포하거나, 큐를 드레인한 뒤 배포할 것. (코드 리뷰에서 발견 — 최초 요약에는 누락되어 있었다) |
| **리포트 JSON 외부 계약 변경** | `FrameworkLabels` 값이 `[]int` → `[]FindingKey`. `SchemaVersion: 2`로 구분 가능. 기존 소비자는 갱신 필요 |
| **`--findings-ttl` 사용 시 경고** | TTL을 켜면 findings만 만료되고 **report는 남는다** → findings 없는 report. `reports` 컬렉션에도 같은 TTL을 걸어야 한다 (배포 노트) |
| **URL은 여전히 평문** | 인덱스·재개·감사·귀속에 필요해 암호화 불가. 쿼리스트링 토큰이 있으면 노출된다 — TLS + 최소권한 + 로그 redaction이 유일한 방어 (`data-census.md` §6) |
| **`ScopeKind` 제로값** | 이름 저장은 했으나 sentinel은 도입하지 않았다(라우팅 분기 전면 수정 필요) |
| **워커 최소권한 불가** | §9 — 워커가 finalize하므로 findings 컬렉션 쓰기 권한이 필요하다 |

---

## 11. 파일

**생성**: `internal/crypto/{envelope,aad,envelope_test}.go` · `internal/domain/{codec,codec_test}.go` ·
`internal/findings/store/{store_mongo,retry,contract_test,retry_test,store_mongo_test}.go` ·
`internal/scan/fetch/{cache_valkey,stats,cache_contract_test,failopen_test,cache_valkey_test}.go` ·
`internal/platform/{metrics}.go` · `internal/platform/orchestrator/persistence_test.go`

**수정**: `internal/domain/{finding,har,severity,coverage,ids}.go` · `internal/findings/store/store.go` ·
`internal/findings/report/report.go` · `internal/scan/fetch/{cache,fetch}.go` · `internal/scan/exec/exec.go` ·
`internal/platform/{types,seams}.go` · `internal/platform/taskstore/{taskstore,taskstore_mongo}.go` ·
`internal/platform/orchestrator/{orchestrator,finalize,stop}.go` · `internal/platform/api/{api,introspect}.go` ·
`cmd/chiller/main.go` · `deploy/compose/docker-compose.yml` · `deploy/k8s/{backends,manager,worker,networkpolicy,secret.example}.yaml`
