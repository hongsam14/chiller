# Data Census — 저장되는 모든 데이터 전수조사

**Date**: 2026-07-13 · **Updated**: 2026-07-13 (구현 완료 후 실제 상태 반영 — G-NEW=A: 이 문서는 **살아있는 대장**이다)
**동기**: "이러다가는 동일 데이터의 사본들이 계속 생겨날거야" — G1(원장의 evidence 평문)이 **개별 버그가 아니라 구조적 증상**이라는 판단.
**방법**: 코드에서 durable 싱크로 나가는 **모든 쓰기 경로**를 역추적 (`bson:` 태그 보유 타입 · 큐 인코딩 · 파일시스템 쓰기 · 로그).
**범위**: 현재(shipped) + 이번 증분에서 추가 예정(proposed).

---

## 1. 저장 싱크 전수 (durable sink inventory)

| # | 싱크 | 쓰는 주체 | 내용 | 상태 |
|---|---|---|---|---|
| **S1** | Mongo `scans` | manager | `Scan{ID, State, Request{URLs, Approved, Options}, Frameworks, CreatedAt, UpdatedAt, ~~Report~~, SchemaVersion}` | shipped |
| **S2** | Mongo `workledger` | **worker** + manager | `WorkLedgerEntry{ID, ScanID, Item{Scope, Target{URL,Host,Scheme}}, State, **Outcome{Findings[]…Evidence}**, LeaseOwner, LeaseExpiry}` | shipped |
| **S3** | Mongo `reports` | manager (finalize) | `report.Report{ScanID, **Findings[]…Evidence**, Coverage, MappingRevision, FrameworkLabels}` | shipped |
| **S4** | Valkey-queue stream | manager (enqueue) | `JSON(domain.WorkItem)` = `{id, scanId, scope, target{url,host,scheme}}` + `scanId` 필드 | shipped |
| **S5** | Valkey-queue drained SET | manager | `scanId` 만 | shipped |
| **S6** | Mongo `findings` | finalize 주체(= 마지막 항목을 비운 worker 또는 manager) | `Finding{cwes, targetUrl, sourceTool, severity, **Evidence(봉인)**, idx, key, schemaVersion, createdAt}` | ✅ **shipped** |
| **S7** | Valkey-cache | **worker** (fetch) | `AES-256-GCM(JSON(HAREntry))` — 값 전체 봉인. 키는 평문 `chiller:cache:{scanId}:{sha256(url)}` + 스캔별 인덱스 SET | ✅ **shipped** (TTL 6h + 종료 시 wipe) |
| **S8** | 파일시스템: Playwright `storageState` | **chiller가 쓰지 않음** — 외부에서 제공, **읽기 전용 입력** (`--storage-state`) | 인증 세션(쿠키·토큰) | shipped (입력) |
| **S9** | 로그 (stdout) | 전체 | 11개 호출 지점. **R9 redaction 규율 적용 대상** | shipped |
| **S10** | 설정 파일 | — (읽기 전용) | `configs/mappings/*.yaml`, `configs/coverage/*.yaml` — 민감정보 없음 | shipped |
| **S11** | 프로세스 메모리만 | — | ResponseCache mem, progress 카운터, FindingsStore mem | shipped |
| **(S12)** | **(미래) probe CLI 임시 파일** | probe exec | 실제 바이너리(testssl/dirhunt/…)가 도입되면 **임시 파일·출력 파일이 생길 수 있음** | infra-plan §3 |

**파일시스템 쓰기 없음 확인**: `os.WriteFile`/`os.Create`/`CreateTemp` 호출 **0건**. S12는 미래 위험으로만 등재.

---

## 2. 사본 조사 (같은 데이터가 몇 벌 존재하는가)

### 🔴 D1 — `Finding` (+`Evidence`): **3벌** ← 문제의 핵심

| 사본 | 싱크 | 암호화 | 왜 존재하는가 |
|---|---|---|---|
| 1 | `workledger.outcome.findings` (S2) | ✅ **봉인** → **finalize 시 소멸** | 원장이 findings의 전송 경로다. 전송이 끝나면(= findings 저장 성공) 사본도 끝난다 (규칙 O-2) |
| 2 | ~~`reports.report.findings` (S3)~~ | ✅ **저장하지 않음** | report는 골격만 저장. findings는 S6이 소유 (규칙 O-1) |
| 3 | `findings` 컬렉션 (S6) | ✅ **봉인** | **정본 — 정상 종료 후 유일한 사본** |

**✅ 해소됨 (G1′=E)**: 정상 종료 후 evidence 사본 = **1벌**(S6). 평문 사본 = **0벌**. 실패·중단 스캔의 원장에는 봉인된 evidence가 남지만 **재개에 필요**하다.

**근본 원인**: 원장이 **상태 추적(resume)** 과 **결과 전송(worker→manager)** 이라는 **두 역할을 겸한다.**
결과를 나르는 한, 결과는 원장에 durable하게 남는다.

**중요한 관찰**: **원장의 findings는 finalize 이후 완전히 무용지물이다.**
원장의 정당한 목적(pending/inflight 재개, lease)은 **모든 항목이 terminal이 되는 순간 끝난다.**
finalize가 성공한 뒤 원장에 남은 `outcome.findings`는 **아무도 읽지 않는 죽은 평문 사본**이다.
(재개는 미완료 항목만 다시 돌린다 — 이미 완료된 항목의 findings는 재개 시 원장에서 읽지만,
그건 **스캔 진행 중**에만 필요하다.)

### 🟡 D2 — `Target` / URL: **3~4벌** (구조적으로 정당)

| 사본 | 싱크 | 성격 |
|---|---|---|
| 1 | `scans.request.urls` + `.approved` (S1) | 원본 요청 (감사 근거) |
| 2 | `workledger.item.target` (S2) | fan-out 전개 결과 (재개의 진실원) |
| 3 | Valkey-queue 스트림 payload (S4) | **전송 중** (일시적, MAXLEN으로 잘림) |
| 4 | `findings.targetUrl` (S6) | 발견의 귀속 대상 (**인덱스 대상 — 평문이어야 함**) |
| (5) | Valkey-cache 키 (S7) | **sha256 해시** — 평문 아님 ✅ |

**판정: 정당.** 각각 다른 목적(요청 감사 / 재개 진실원 / 전송 / 귀속)이고, 어느 하나도 다른 것을 대체할 수 없다.
다만 **URL 자체가 민감할 수 있다**(쿼리스트링 토큰 — `?token=…`, `?sessionid=…`). 현재 **전부 평문**.

### 🟢 D3 — 나머지: 사본 없음

`Coverage`(S3만) · `ScanState`/`ItemState`(각 1곳) · `Lease`(S2만) · `HAREntry`(S7만) · scanId(식별자).

---

## 3. 민감도 등급 (무엇을 지켜야 하는가)

| 등급 | 데이터 | 어디에 | 현재 보호 |
|---|---|---|---|
| 🔴 **HIGH** | `HAREntry.Cookies` — **대상 사이트의 세션 쿠키** | S7 (proposed), S11 | 없음 → 이번 증분에서 **암호화**(Q6=A) |
| 🔴 **HIGH** | `HAREntry.Body` — 응답 본문 전체 (토큰·PII 가능) | S7 (proposed), S11 | 없음 → 이번 증분에서 **암호화** |
| 🔴 **HIGH** | `HAREntry.Headers` — `Set-Cookie`, `Authorization` 반사 등 | S7 (proposed) | 없음 → 이번 증분에서 **암호화** |
| 🔴 **HIGH** | `Evidence.Snippets` — 응답 조각 | **S2(평문!)**, S3(평문), S6(암호화 예정) | **S2가 구멍 = G1** |
| 🔴 **HIGH** | Playwright `storageState` — 인증 세션 | S8 (외부 파일) | **chiller 범위 밖** — 파일 권한·시크릿 관리는 운영. 코드가 쓰지 않음(확인됨) |
| 🟡 **MED** | URL / Target (쿼리스트링 토큰 가능) | S1, S2, S3, S4, S6 | 전부 평문. **인덱스·재개·감사에 필요해 암호화 불가**. → 전송 TLS + 최소권한 + 로그 redaction으로 방어 |
| 🟢 LOW | scanId, state, lease, CWE, severity, coverage, 카운터 | 전역 | 보호 불요 |

---

## 4. 소유권 규칙 (사본 증식을 막는 구조)

> **규칙 O-1 (단일 소유자)**: 모든 데이터 항목은 **정확히 하나의 싱크가 소유**한다. 다른 싱크는 **참조(키)** 만 갖는다.
> **규칙 O-2 (전송 ≠ 저장)**: 전송 목적의 사본(큐 payload, 원장의 outcome)은 **목적이 끝나는 시점에 반드시 소멸**한다.
> **규칙 O-3 (새 싱크 신고제)**: 새 저장 싱크를 추가하려면 이 표에 **소유자·사본 정당화·민감도·암호화 여부**를 먼저 등재한다.
> **규칙 O-4 (민감 데이터는 소유자만 평문)**: 🔴 HIGH 데이터는 **소유 싱크에서만** (암호화된 채로) 존재한다. 경유지에 잔류하지 않는다.

### 소유권 표 (이번 증분 이후의 목표 상태)

| 데이터 | 소유 싱크 | 참조만 갖는 곳 | 경유(소멸 시점) |
|---|---|---|---|
| `Scan` 요청·상태 | `scans` (S1) | — | — |
| WorkItem 원장·lease | `workledger` (S2) | — | — |
| **`Finding` + `Evidence`** | **`findings` (S6)** | `reports.frameworkLabels` → `FindingKey` (C2=B) | **`workledger.outcome` (finalize 시 소멸)** · Valkey 큐(전송, XACK 후 소멸) |
| report 골격 (coverage/labels) | `reports` (S3) | — | — |
| `HAREntry` | Valkey-cache (S7) | — | **스캔 종료 시 wipe + TTL** (Q3=A) |
| WorkItem (전송) | — | — | Valkey 큐 스트림 (MAXLEN + XACK) |

**✅ 구현 완료**: `Evidence`의 평문 사본은 **0벌**(진행 중에도 원장은 봉인되어 있다). 정상 종료 후 사본 자체가 **1벌**(S6)로 수렴한다.

**규칙 O-3 발효**: 새 저장 싱크를 추가하려면 **이 표에 먼저 등재**한다. 첫 적용 대상 = infra-plan §3 probe CLI 임시 파일(S12).

---

## 5. G1 재검토 — 전수조사가 바꾸는 것

원래 G1의 선택지는 "원장도 암호화할까?"였다. 전수조사 결과 **더 나은 선택지가 드러났다**:
원장의 findings는 **finalize 이후 죽은 데이터**이므로, **암호화**와 **소멸**을 함께 쓰면 사본 문제가 구조적으로 닫힌다.

- **A) 원장도 봉투 암호화** — 평문 사본 0. 그러나 **죽은 사본은 그대로 남는다**(암호화된 채). 사본 수는 여전히 2벌.
- **E) 원장 암호화 + finalize 시 원장 outcome의 evidence 소멸 (권장, A+O-2)** —
  ① `taskstore.Mongo`가 `RecordOutcome`/`AllOutcomes`에서 evidence를 `Seal`/`Open` (진행 중에도 평문 없음)
  ② **finalize가 findings 컬렉션 저장에 성공한 직후, 원장의 `outcome.findings[].evidence`를 비운다**
     (원장의 나머지 — id/failed/notApplicable/상태/lease — 는 감사·재개를 위해 보존)
  → **정상 종료 후 evidence 사본은 `findings` 컬렉션 1벌.** 규칙 O-1·O-2·O-4를 모두 만족.
  → 실패·중단된 스캔의 원장에는 암호화된 evidence가 남지만, 그건 **재개를 위해 필요**하다.
- **F) E + 원장 TTL** — 추가로 원장 문서 자체에 TTL. (수명 정책이므로 별건 — Q7과 같은 축)

**비용**: finalize에 쓰기 1회 추가(`updateMany`로 evidence 필드 unset). 실패해도 스캔 결과에 영향 없음(로그+카운터).

---

## 6. 남은 노출 (이번 증분으로도 못 지우는 것 — 정직한 기록)

| 항목 | 왜 남는가 | 완화 |
|---|---|---|
| **URL 평문** (S1·S2·S3·S4·S6) | 인덱스·재개·감사·귀속에 필요 — 암호화하면 기능이 죽는다 | TLS in-transit · 컬렉션/키 최소권한 · **로그 redaction**(R9) · 쿼리스트링 토큰이 걱정되면 ingest에서 정규화(별건) |
| **진행 중 스캔의 원장 evidence** | 재개를 위해 필요 | 봉투 암호화(E) — 평문은 없음 |
| **실패/중단 스캔의 원장 evidence** | 재개를 위해 보존 | 봉투 암호화 + (선택) 원장 TTL |
| **워커 프로세스 메모리의 평문** | fetch·regex 탐색에 불가피 | 범위 밖 (프로세스 격리·메모리 덤프 통제는 운영) |
| **`storageState` 파일** | 외부 입력, chiller가 쓰지 않음 | 파일 권한·시크릿 관리(운영). **코드가 이 파일을 다른 곳에 복사하지 않음을 확인함** |
| **(미래) probe 임시 파일** (S12) | 실제 CLI 바이너리 도입 시 | infra-plan §3에서 **이 census에 등재하고 설계**할 것 (규칙 O-3) |

---

## 7. 결론 — 답변이 필요한 질문 (갱신)

전수조사를 반영해 **G1을 재구성**한다. G2는 변경 없음.

### G1′ — 원장의 evidence 사본 처리

- **A) 원장도 봉투 암호화** — 평문 0, 사본 2벌 유지
- **E) 원장 봉투 암호화 + finalize 시 원장 evidence 소멸 (권장)** — 평문 0, **정상 종료 후 사본 1벌**. 규칙 O-1/O-2/O-4 만족
- **F) E + 원장 문서 TTL** — E에 수명 정책 추가
- **D) 평문 수용** — SECURITY-01 미충족 명시 인정 (비권장)

[Answer]: E

### G2 — int-iota enum 전수 (변경 없음, gap-analysis.md 참조)

- **A) `Severity` + `ItemStatus` + `ScopeKind` 전부 문자열 마샬링 + `ItemRecord` bson 태그 (권장)**
- **B) `Severity`만** · **C) `ScopeKind` 제외**

[Answer]: A

### G-NEW — 데이터 소유권 규칙(§4 O-1~O-4)을 프로젝트 규율로 채택할 것인가

- **A) 채택 — 이 문서를 살아있는 대장(ledger)으로 유지하고, 새 저장 싱크는 반드시 여기 먼저 등재 (권장)**
  (`aidlc-state.md`에서 참조하고, infra-plan §3 probe 작업이 첫 적용 대상)
- **B) 이번 증분 한정으로만 사용하고 규율화하지 않음**

[Answer]: A
