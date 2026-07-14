# Gap Analysis — 설계 ↔ 코드 생성 플랜 대조

**Date**: 2026-07-13
**대조 대상**: `construction/persistence/infrastructure-design/*` (승인됨) ↔ `construction/plans/persistence-code-generation-plan.md`
**방법**: 설계 문서의 모든 결정을 항목화해 플랜의 20단계에 매핑. 매핑되지 않는 항목 + **설계 자체가 놓친 것**을 코드로 역검증.

**결과**: 플랜 누락 6건(**G3–G8**, 결정 불필요 — 플랜에 반영) + **설계 누락 2건(G1·G2, 결정 필요 — blocking)**

---

# 🔴 G1 (BLOCKING) — evidence의 **세 번째 평문 사본**: `workledger`

## 설계가 놓친 것

설계 §3.6은 "evidence를 `findings` 컬렉션에 암호화해 저장"한다고 했다. 그런데 코드를 보면:

```go
// internal/domain/workitem.go:19
type WorkItemOutcome struct {
    ID            WorkItemID          `bson:"id"`
    Findings      []Finding           `bson:"findings"`   // ← Evidence.Snippets 포함
    Failed        []CheckerID         `bson:"failed"`
    NotApplicable map[CheckerID][]CWE `bson:"notApplicable"`
}

// internal/platform/types.go:132  (WorkLedgerEntry)
Outcome *domain.WorkItemOutcome `bson:"outcome"`   // ← workledger 컬렉션에 그대로 저장됨
```

**`workledger` 컬렉션의 모든 완료 항목 문서에 evidence snippet이 평문으로 durable하게 들어 있다.**
직전 platform 증분에서 이미 그렇게 저장되고 있다.

더 중요한 구조적 사실: **원장이 findings의 전송 경로다.** worker가 outcome을 원장에 기록 → manager의 `finalize`가
`AllOutcomes`로 읽어 dedup → `findings` 컬렉션에 저장. 즉 findings는 **반드시 원장을 거친다.**

```
worker ──RecordOutcome──▶ workledger.outcome.findings[].evidence  ← 평문 (오늘)
                                    │
                          finalize: AllOutcomes
                                    ▼
                          dedup ──▶ findings 컬렉션.evidence     ← 암호화 (설계된 것)
```

**결론**: `findings` 컬렉션만 암호화하면 **SECURITY-01(at-rest 암호화)은 여전히 미충족**이다.
같은 evidence가 옆 컬렉션에 평문으로 남는다. 이 증분의 목표(§9 결함 ②)가 절반만 달성된다.

## 어떻게 할 것인가

- **A) `taskstore`의 원장 쓰기/읽기에도 동일한 봉투 암호화를 적용 (권장)** — `taskstore.Mongo`가
  `RecordOutcome` 시 `outcome.findings[].evidence`를 `Seal`(AAD = scanId + itemId + findingKey),
  `AllOutcomes`/`AllItems` 시 `Open`. `internal/crypto`를 두 리포지토리(taskstore, findings store)가 **공유**한다.
  암호화 지점이 2곳이지만 **둘 다 명시적**이고, 각자 자기 AAD로 바인딩된다. `taskstore.Mem`은 암호화하지 않는다(메모리).
  → 플랜에 **Step 추가** 필요 (Phase B 또는 C).
- **B) `domain.Evidence`가 `bson.ValueMarshaler`/`ValueUnmarshaler`를 구현해 **어디에 저장되든 자동 암호화**** —
  암호화 지점이 **한 곳**이라 누락이 구조적으로 불가능하다. 그러나 domain이 **프로세스 전역 키링(숨은 전역 상태)** 에
  의존하게 되고, AAD를 scanId/key에 바인딩할 수 없다(마샬러는 문맥을 모른다 → 봉투 이식 차단 상실).
  domain 커널이 crypto를 import하는 것도 레이어 원칙(arch-check)에 부담.
- **C) 원장에서 evidence를 제거** — outcome은 evidence 없는 finding만 나르고, evidence는 worker가 `findings` 컬렉션에
  직접 쓴다. 원장이 가벼워지지만 **worker가 findings 컬렉션 쓰기 권한을 갖게 되고**(최소 권한 약화),
  dedup 전 데이터가 정본 컬렉션에 들어가며, 부분 실패 시 정합성 관리가 복잡해진다. **비권장.**
- **D) 원장의 평문을 수용** — TTL로 원장을 조기 만료시키는 것으로 완화. SECURITY-01 미충족을 명시적으로 인정하는 것.

[Answer]:

---

# 🔴 G2 (BLOCKING) — int-iota enum이 **`Severity` 하나가 아니다**

## 설계가 놓친 것

설계 Q4=A는 **`Severity`만** 문자열 마샬링 대상으로 지목했다. 코드를 전수 확인한 결과 **같은 결함을 가진 enum이 2개 더 있다**:

| 타입 | 정의 | 저장 위치 | 상태 |
|---|---|---|---|
| `domain.Severity` | `int` iota | `reports.report.findings[].severity` | 설계가 잡음 (Q4=A) |
| **`domain.ItemStatus`** | `int` iota (`coverage.go:7`) | **`reports.report.coverage[PS-xxx].status`** | **설계가 놓침** |
| **`domain.ScopeKind`** | `int` iota (`ids.go:4`) | **`workledger.item.scope`** | **설계가 놓침** (직전 platform 증분도 놓침) |
| `platform.ScanState` / `ItemState` | int iota | `scans.state` / `workledger.state` | 직전 증분에서 **문자열로 이미 수정됨** |

추가로 `domain.ItemRecord`(`{Status, Target}`)에는 **bson 태그가 없다**.

**같은 잠재 결함이다**: 상수 순서가 바뀌면 기존 문서가 조용히 오염된다. `ItemStatus`는 특히 위험하다 —
`StatusChecked`가 `iota`의 **0번**이라 **필드가 누락된 문서도 `StatusChecked`로 읽힌다**(BSON 기본값 0).
즉 "저장 실패로 status가 빠진 문서"가 "검사 완료"로 둔갑한다. 컴플라이언스 보고서에서 이건 **오탐이 아니라 미탐**이다.

## 어떻게 할 것인가

- **A) Q4=A의 정책을 int-iota enum **전부**로 확장 (권장)** — `Severity` + **`ItemStatus`** + **`ScopeKind`** 에
  문자열 마샬러 + 파서(미지 문자열 → **에러**), `ItemRecord`에 bson 태그 추가. 프로덕션 데이터가 없는 **지금이 유일하게 싼 시점**.
  플랜 Step 1의 범위를 넓힌다.
- **B) `Severity`만 (설계대로)** — 나머지 둘은 별도 작업으로. 잠재 오염 + `ItemStatus` 0-기본값 위험 존치.
- **C) `Severity` + `ItemStatus`만, `ScopeKind`는 제외** — `ScopeKind`는 원장 내부용(외부 계약 아님)이라는 논리.
  다만 원장도 durable이고 resume 경로가 읽는다.

[Answer]:

---

# 🟡 G3–G8 — 플랜 누락 (결정 불필요, 플랜에 반영하면 됨)

승인된 설계에는 있으나 코드 생성 플랜의 단계에 명시되지 않은 항목들. 답변 없이 플랜을 갱신한다.

| # | 누락 | 설계 근거 | 반영할 곳 |
|---|---|---|---|
| **G3** | **report 조립 함수는 `Findings`를 항상 `FindingsStore` 것으로 덮어써야 한다.** `taskstore.Mem`은 findings를 그대로 들고 있고 `Mongo`는 비운 채 돌려주므로, 덮어쓰지 않으면 **백엔드 조합에 따라 동작이 달라진다**(mem+mongo 혼합 시 조용한 불일치). 조립 지점은 `orchestrator.go:177 GetReport` **한 곳**임을 코드로 확인 | 설계 §3.4 경계 규칙 | Step 11 — "**무조건 덮어쓴다**" 명시 + 백엔드 조합 무관 테스트 |
| **G4** | **role별 배선.** worker는 캐시를, manager는 FindingsStore를 쓴다. **cross-process 재사용(FR-02/US-002)은 모든 워커가 같은 Valkey-cache를 가리켜야만 성립**한다 — 이게 이 증분이 US-002에 주는 실질 가치인데 플랜에 조건이 안 적혀 있다. `--role=worker`가 `--findings-store` 없이 뜰 수 있는지도 미정 | 배포 설계 §1 | Step 16 — role별 필수/선택 백엔드 매트릭스 + 워커 공유 캐시 요구 명시 |
| **G5** | `--findings-store=mongo`는 **기존 `--mongo-uri`/`CHILLER_MONGO_URI`를 재사용**한다(인스턴스 공유, Q9=C). 별도 URI 플래그를 만들지 않는다 | 설계 §7, shared-infra §1 | Step 16 |
| **G6** | **`store.InMemory`는 암호화하지 않는다** (프로세스 메모리). 키링은 Mongo 구현에만 주입 | 설계 §3.6 | Step 5/6 |
| **G7** | **`reports` 컬렉션 TTL 경고**: `--findings-ttl`을 켜면 findings만 만료되어 **report가 findings 없이 남는다**. 운영자에게 동일 TTL을 `reports`에도 적용하라고 경고해야 함 | 설계 §3.7 | Step 19/20 (배포 노트 + 요약 문서) |
| **G8** | `SaveFindings(ctx, …)` 호출부 갱신(`finalize.go`)이 단계 본문에 없음 (파일 목록엔 있음) | 설계 §2.3 | Step 5 |

---

## 매핑 검증 — 설계 항목 전수 대조 (누락 없음 확인)

| 설계 항목 | 플랜 단계 |
|---|---|
| §0 F-1″ 죽은 `Scan.Report` 제거 | Step 10 ✅ |
| §2.1 FindingsStore ctx + **순서 보존 계약** | Step 5 ✅ |
| §2.2 ResponseCache ctx + `Get` 3-값 | Step 12 ✅ |
| §2.3 호출부 4곳 | Step 5(G8 보강)·12·16 ✅ |
| §3.1/§3.2 전용 컬렉션·스키마·인덱스·멱등 upsert·꼬리 삭제·majority WC | Step 6 ✅ |
| §3.3 bson 태그 + enum 문자열 (**G2로 범위 확대**) | Step 1 ⚠️ |
| §3.4 report에서 findings 비우기 + 경계 규칙 | Step 10·11 (**G3 보강**) ⚠️ |
| §3.5 FindingKey + dedup 후 유일성 PBT | Step 2·9·15 ✅ |
| §3.6 봉투 암호화·AAD·키 회전·fail-closed (**G1로 범위 확대**) | Step 3·6 ⚠️ |
| §3.7 TTL 기본 무기한 (**G7 보강**) | Step 6 ⚠️ |
| §3.8 재시도 3회 백오프, 일시 오류만 | Step 7 ✅ |
| §4.1 캐시 키/값/TTL/AAD/인덱스 SET | Step 13 ✅ |
| §4.3 WipeScan 3경로 배선 + TTL 백스톱 | Step 14 ✅ |
| §4.4 fail-open + 카운터 | Step 12·15 ✅ |
| §5 카운터 8종 + healthz + introspect + UI | Step 17 ✅ |
| §6 계약 스위트·PBT 4종·통합·F-4 회귀 | Step 4·8·15·18 ✅ |
| §7 composition root 플래그 (**G5 보강**) | Step 16 ⚠️ |
| 배포 §4 compose·k8s·secret 템플릿 | Step 19 ✅ |
| 배포 §5 장애 격리 / §6 공유 Mongo 트레이드오프 | Step 12·15(fail-open) / 문서 ✅ |

---

## 다음 단계

**G1·G2 답변** → 플랜 갱신(G1은 신규 Step, G2는 Step 1 확대, G3–G8은 답변 없이 반영) → Part 2 코드 생성.
