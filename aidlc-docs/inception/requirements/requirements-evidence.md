# Requirements — Report 개선: Evidence 강화 (r20)

**Stage**: Requirements Analysis · **Depth**: Standard · **Type**: Brownfield increment
**Date**: 2026-07-29 · **Branch (예정)**: feat/evidence-enrichment (off main)
**Status**: DRAFT — 승인 대기

---

## 1. Intent (사용자 의도)

원문: *"ai-dlc로 report개선작업. evidence 강화"*

명확화 라운드에서 드러난 **진짜 목표**(Q1 note):

> "이 정보를 기반으로 **AI가 능동 공격 플랜**을 세울건데, 현재는 evidence가 없음으로 **정오탐 체크가 불가능**함"

즉 evidence 강화는 **고객 UI 개선이 아니라**, passive-scan finding을 소비하는 **다운스트림 AI(능동 공격 플래너 + 정오탐 체커)** 를 위한 **기계 판독용 근거 강화**다. AI가 finding당 다음을 할 수 있어야 한다:

1. **정오탐(FP/FN) 판단** — 이 passive 신호가 실제 취약인지, 오탐인지.
2. **능동 공격 플랜 수립** — passive→active 로 재현/피벗할 진입점 확보.

### 현재 상태 (실코드 조사 결과)

- **모델**: `domain.Evidence{ Snippets []string }` — bounded(≤`MaxEvidenceSnippets`=8), 각 ≤`maxEvidenceLen`=200자, whitespace-collapsed, redacted, at-rest 암호화(`crypto.Envelope`, AAD-bound), 스캔 후 ledger purge.
- **캡처**: observation은 매치 바이트 + 소량 컨텍스트 창(`matchContext`); probe는 CLI stdout clip.
- **리포트**: `findingDTO.Evidence []string` → `/report` 드릴다운 `<pre>` 렌더.
- **갭**: snippet이 bare 문자열이라 (a) **어디서** 매치됐는지(헤더 vs 바디, 오프셋), (b) **어떤 룰**이 발화했고 무엇을 탐지하려 했는지(가설), (c) **요청/응답 컨텍스트**(method/URL/status/헤더/truncated)가 전무 → AI가 재현/피벗/오탐판정 불가.

### 실현 가능성 (grounding)

- observation 시그니처는 `Signature.Judge(domain.ScanContext)` 를 통해 `ctx.Response *domain.HAREntry` 를 이미 받는다. `HAREntry` 는 `URL/Scheme/Status/Headers/Cookies/Body/FetchedAt/Truncated` 를 모두 보유 → **요청/응답 컨텍스트 전 필드가 캡처 시점에 이미 접근 가능**. 신규 fetch/전파 없음.
- probe finding 은 외부 CLI stdout 파싱이라 HAREntry 없음 → **best-effort**(tool 이름 + raw line), 없는 필드는 **날조 금지**(정직성).

---

## 2. Decisions (명확화 확정)

| # | 결정 | 근거 |
|---|---|---|
| **D1** | 소비자 = **다운스트림 AI**(능동 공격 플래너 + 정오탐 체커). 기계 판독용, 고객 UI 아님. | Q1 note |
| **D2** | **필드 4종 전부**: 매치 위치 · 룰/시그니처 귀속+가설 · 요청 컨텍스트 · 응답 컨텍스트. | Q-A(4개 다중선택) |
| **D3** | 적용 범위: **observation 완전 구조화, probe best-effort**(없는 필드 날조 금지). | Q2 |
| **D4** | 저장 스키마 **교체**, 마이그레이션 없음(기존 리포트는 재스캔). | Q3 |
| **D5** | 노출 면 = **기존 `/report` findingDTO 확장**(전용 엔드포인트 아님). | Q-B |

---

## 3. Functional Requirements

### 데이터 모델
- **FR-E1** `domain.Evidence` 를 bare `Snippets []string` 에서 **구조화된 evidence item 목록**으로 교체한다. 기존 필드는 유지하지 않는다(D4, 마이그레이션 없음).
- **FR-E2 (매치 위치)** 각 evidence item 은 **매치 위치**를 기록한다: 응답 부위(`header` | `body` | `cookie`), 헤더인 경우 헤더명, 바디인 경우 바이트/라인 오프셋, 그리고 snippet 내 **매치된 substring 의 경계**(AI 가 컨텍스트와 실제 매치를 구분하도록).
- **FR-E3 (룰 귀속 + 가설)** 각 item 은 **발화한 룰/시그니처 ID**(예: `observation:CWE-615`, 또는 config 룰의 세부 id)와 **가설**(그 룰이 무엇을 탐지하려 하는지 — CWE 근거/의미)을 기록한다. AI 능동 검증 가설의 출발점.
- **FR-E4 (요청 컨텍스트)** evidence 는 **요청 컨텍스트**를 기록한다: HTTP method, 패치한 전체 URL, 주요 요청 헤더(bounded). passive→active 재현/피벗 진입점.
- **FR-E5 (응답 컨텍스트)** evidence 는 **응답 컨텍스트**를 기록한다: HTTP status, Content-Type, 관련 응답 헤더, `Truncated` 플래그. FP 판정 및 재현 조건.

### 캡처 (계열별)
- **FR-E6 (observation 완전)** observation 13항목은 FR-E2~E5 를 `ScanContext.Response`(HAREntry)로부터 **완전 채운다**. 신규 fetch 없음.
- **FR-E7 (probe best-effort·정직)** probe 9항목은 HAREntry 가 없으므로 채울 수 있는 필드만(tool 이름 = 룰 귀속, raw stdout line = snippet) 채우고, **없는 필드는 생략한다**(zero-fill/날조 금지). "관측 안 한 것을 관측한 척" 금지 — 기존 정직성 불변.

### 경계 · 보안 (security-baseline, blocking)
- **FR-E8 (경계 보존)** item 수 상한(`MaxEvidenceSnippets`=8)과 필드별 길이 상한(snippet ≤200 등)을 **유지**한다. 신규 필드(URL, 헤더값)도 각각 bounded. 무제한 페이지/헤더 덤프 금지(FR-12/NFR-16).
- **FR-E9 (at-rest 암호화 유지)** 확장된 evidence 전체가 기존대로 at-rest 암호화(`crypto.Envelope`, `EvidenceAAD`/`LedgerEvidenceAAD` 바인딩)로 봉인된다. seal/open, ledger purge(`PurgeOutcomeEvidence`)가 신규 구조를 온전히 왕복.
- **FR-E10 (민감정보 취급)** 요청 헤더(`Authorization`/`Cookie` 등)·세션 토큰(PS-018)은 AI 정오탐 판정에 **필요하므로 보존**하되, 오직 암호화된 at-rest 로만 저장하고 로그에는 redact(SECURITY-03). 이 trade(민감정보를 evidence 에 담음)를 문서화한다.

### 파이프라인 · 노출
- **FR-E11 (normalize/dedup)** `findings/normalize`·`findings/dedup` 의 evidence 병합/경계 로직을 신규 구조로 갱신한다. dedup union 은 여전히 `MaxEvidenceSnippets` 로 bounded, 병합은 결정적·멱등(PBT 대상).
- **FR-E12 (리포트 노출)** `/report` 의 `findingDTO` 에 구조화 필드를 노출한다. 고객 요약 카운트(vulnerable/clean/…)와 파티션은 **불변**. HTML 뷰(`report_ui.go`)는 최소 표시만(주 소비자는 JSON DTO).

### 불변 (건드리지 않음)
- **FR-E13** 26-항목 파티션, checker↔CWE 소유권, probe 정직성(unwired→tool-failed), 안전 봉투(approval∩allowlist·rate·timeout), 제품 Dockerfile, platform seam 인터페이스는 불변.

---

## 4. Non-Functional / Extension 준수

| Extension | Enabled | 적용 | 상태 |
|---|---|---|---|
| **security-baseline** | Yes (blocking) | 확장 evidence(요청/응답 헤더·URL·토큰)의 경계·redaction·at-rest 암호화. | FR-E8/E9/E10 로 처리 |
| **resiliency-baseline** | Yes (blocking) | 캡처는 total(panic 금지, SR-6). probe best-effort 는 파이프라인을 실패시키지 않음. 캡처 실패가 finding 유실로 이어지지 않음. | 설계 시 준수 |
| **property-based-testing** | Yes (FULL) | PBT: (P) item 수 ≤ 상한 불변, (P) 필드 길이 ≤ 상한 불변, (P) dedup 병합 멱등·결정적, (P) probe evidence 는 날조 필드 없음, (P) seal→open 왕복 = 항등. | Code Gen/Build&Test 에서 |

---

## 5. 영향 범위 (touch map — 참고용, 상세는 Functional Design)

- `internal/domain/finding.go` — Evidence 구조 교체 + 경계 상수.
- `internal/domain/checker.go`·`har.go` — (읽기 전용, 신규 타입 없을 수 있음; FD 확정).
- `internal/scan/checking/observation/{signatures.go, signatures_config.go, wellknown.go, cookie*}` — 캡처 시 위치/룰/가설/요청/응답 채움.
- `internal/scan/checking/probe/parsers.go` — best-effort 필드만.
- `internal/findings/normalize`·`dedup` — 신규 구조 경계/병합.
- `internal/findings/store/store_mongo.go`·`internal/platform/taskstore/taskstore.go` — seal/open·purge 신규 구조.
- `internal/platform/api/report_view.go`(+`report_ui.go`) — findingDTO 확장.

---

## 6. Out of scope (정직한 이월)

- **전용 machine/AI 엔드포인트** — Q-B 에서 기존 /report DTO 확장 선택. 별도 엔드포인트는 미포함.
- **HTML 리포트 UI 고도화**(하이라이트/헤더 배지 등) — 주 소비자가 JSON 이므로 최소만.
- **능동 공격 플래너 자체** — 별도 스테이지(AI-Active-Scanner). 본 증분은 그 소비자를 위한 evidence 공급만.
- **발견/서브도메인 스코프**, **changeme(PS-019)** — 기존 backlog 유지.

---

## 7. 승인 게이트

위 요구사항(D1~D5, FR-E1~E13, extension 준수)이 의도와 일치하는지 확인 부탁드립니다.

- **(A) 승인 & 계속** → Workflow Planning 으로 진행
- **(B) 변경 요청** → 어떤 항목을 조정할지 알려주시면 반영
