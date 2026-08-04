# Functional Design — Evidence 강화 (r20)

**Stage**: Functional Design · **Increment**: r20 · **Branch**: feat/evidence-enrichment
**Requirements**: `inception/requirements/requirements-evidence.md` (approved)
**Plan**: `inception/plans/execution-plan-evidence.md` (approved)
**Status**: DRAFT — 승인 대기

이 증분은 유닛 횡단(domain U0 · scan U2 · findings U3 · platform U1)이라 하나의 FD로 다룬다.

---

## 0. 실코드 grounding (설계 근거)

- **live 경로**: observation/probe 체커가 `domain.Finding` 을 **직접** 생성(`observation.finding()` / `probe.pFinding()`) → `CheckResult` → `exec` → `orchestrator.finalize` 가 `Deduper.Dedup` → store. `normalize.NormalizeAll`(RawFinding 경로)은 **live 미배선**(dormant seam)이나 `domain.Evidence` 를 참조하므로 컴파일 위해 갱신 필요.
- **캡처 가용 데이터**: observation `Signature.Judge(ctx)` 는 `ctx.Response *domain.HAREntry`(URL/Scheme/Status/Headers/Cookies/Body/FetchedAt/Truncated) + `ctx.DiscoveredJS []*HAREntry` 접근. 바디 매치 오프셋은 `re.FindIndex(body)` 로 이미 가용(현재 버림).
- **fetcher**: 항상 `GET`, 유일 요청 헤더 `Accept-Encoding: gzip,deflate,br,zstd`. 요청 헤더는 HAREntry에 미기록 → Method+URL만 확실. (설계 D5 참조)
- **영속성**: `store_mongo.sealEvidence` 는 `json.Marshal(ev.Snippets)` → `crypto.Seal(…, EvidenceAAD)`; `taskstore_mongo` 는 ledger 용 `LedgerEvidenceAAD` 로 동일 패턴. 둘 다 **`ev.Snippets` 만** 직렬화 → 신규 구조로 교체 필요. in-mem `store.keepExistingEvidence`(never-erase)와 `taskstore.PurgeOutcomeEvidence`(zero화)도 갱신.
- **DTO/UI**: `report_view.findingDTO.Evidence []string` → `report_ui.go` JS가 `<pre>` 렌더.
- **파급 지점(16 `.Snippets` 참조, 8 파일)**: signatures.go · parsers.go · normalize.go · dedup.go · store.go · store_mongo.go · taskstore_mongo.go · report_view.go.

---

## 1. FD-1 — 신규 도메인 모델 (`internal/domain/finding.go`)

`Evidence{Snippets []string}` 를 아래로 **교체**(마이그레이션 없음, D4). 모든 문자열은 bounded+redacted, 전체는 at-rest 암호화(불변).

```go
// Evidence: finding의 구조화된 근거. 다운스트림 소비자(AI 능동 공격 플래너/정오탐
// 체커)가 판정·재현할 수 있는 기계 판독 피드. 전체 페이지 덤프 금지(FR-12), 각
// 문자열 bounded+redacted(R9/SECURITY-03), 전체 at-rest 암호화(SECURITY-01).
type Evidence struct {
    Items    []EvidenceItem   `bson:"items,omitempty"    json:"items,omitempty"`    // 개별 매치, ≤MaxEvidenceItems
    Request  *RequestContext  `bson:"request,omitempty"  json:"request,omitempty"`  // 매치가 실제로 나온 교환(F1)
    Response *ResponseContext `bson:"response,omitempty" json:"response,omitempty"` // probe는 보통 nil
}
// NOTE(F1): Request/Response는 "매치가 실제로 나온 소스 교환"을 기술한다 — 항상
// ctx.Response(페이지)가 아니다. sensitiveComment처럼 매치가 발견된 JS(js.URL)에서
// 오면 Request/Response는 그 JS 교환이고 Location.SourceURL=js.URL. 관측 시그니처는
// 현재 첫 매치에서 조기 반환(=finding당 단일 소스)이라 이 귀속은 결정적이다.

// EvidenceItem: 매치 한 조각.
type EvidenceItem struct {
    Snippet    string   `bson:"snippet"              json:"snippet"`              // 실제 매치 내용(bounded ≤maxEvidenceLen)
    Location   Location `bson:"location,omitempty"   json:"location,omitempty"`   // 어디서 매치했는지 (FR-E2)
    Rule       string   `bson:"rule,omitempty"       json:"rule,omitempty"`       // 발화 룰/시그니처 id (FR-E3)
    Hypothesis string   `bson:"hypothesis,omitempty" json:"hypothesis,omitempty"` // 그 룰이 탐지하려는 것(가설, FR-E3)
}

// Location: 매치가 교환의 어디에 있는지 → 소비자가 매치 토큰과 컨텍스트를 구분하고
// 재-패치할 수 있게. 오프셋은 **원본 바디**(정규화 전) 기준(정직: snippet은 공백
// 정규화되어 그 안의 정확한 위치는 신뢰 불가 → 원본 바디 포인터만 제공).
type Location struct {
    Part      MatchPart `bson:"part,omitempty"      json:"part,omitempty"`      // header|body|cookie|url
    Name      string    `bson:"name,omitempty"      json:"name,omitempty"`      // 헤더/쿠키명 (Part=header|cookie)
    Offset    int       `bson:"offset,omitempty"    json:"offset,omitempty"`    // 원본 바디 바이트 오프셋 (Part=body)
    MatchLen  int       `bson:"matchLen,omitempty"  json:"matchLen,omitempty"`  // Offset의 매치 길이(바이트)
    SourceURL string    `bson:"sourceUrl,omitempty" json:"sourceUrl,omitempty"` // 매치가 주 응답이 아닌 URL(예: 발견된 JS)에서 온 경우에만
}

type MatchPart string
const (
    PartHeader MatchPart = "header"
    PartBody   MatchPart = "body"
    PartCookie MatchPart = "cookie"
    PartURL    MatchPart = "url"
)

// RequestContext: passive→active 재현/피벗 진입점 (FR-E4).
type RequestContext struct {
    Method  string   `bson:"method,omitempty"  json:"method,omitempty"`  // 관측: 항상 GET
    URL     string   `bson:"url,omitempty"     json:"url,omitempty"`
    Headers []Header `bson:"headers,omitempty" json:"headers,omitempty"` // bounded; 없으면 생략(D5)
}

// ResponseContext: FP 판정 + 재현 조건 (FR-E5).
type ResponseContext struct {
    Status      int      `bson:"status,omitempty"      json:"status,omitempty"`
    ContentType string   `bson:"contentType,omitempty" json:"contentType,omitempty"`
    Headers     []Header `bson:"headers,omitempty"     json:"headers,omitempty"` // bounded, 관련 응답 헤더
    Truncated   bool     `bson:"truncated,omitempty"   json:"truncated,omitempty"`
}
```

**경계 상수** (`finding.go`):
- `MaxEvidenceItems = 8` — 기존 `MaxEvidenceSnippets` 를 이 이름으로 대체(의미 동일: item 수 상한). 하위 호출부 갱신.
- `maxEvidenceLen`(observation, =200) 유지 — snippet 길이.
- **신규** `MaxEvidenceHeaders = 12` — Request/Response 헤더 리스트 상한.
- **신규** `maxHeaderValueLen = 256` — 헤더 값 상한(무제한 헤더 덤프 방지).

**헬퍼**:
- `func (e Evidence) Empty() bool { return len(e.Items) == 0 && e.Request == nil && e.Response == nil }` — **(F2 교정)** seal 게이트/never-erase 판정 기준. Items 뿐 아니라 Request/Response까지 비어야 empty → 컨텍스트만 있는 evidence도 seal이 보존하므로 seal 왕복 항등(FD-9 #4)이 **무조건** 성립. (실무상 관측·probe 모두 항상 ≥1 Item.)
- `Finding.Validate()` 는 evidence 를 검증하지 않음(현행 유지) — evidence 는 근거이지 identity 아님.

---

## 2. FD-2 — observation 캡처 (완전 구조화, `observation/*`)

`finding()` 헬퍼를 컨텍스트-인지형으로 교체. 시그니처는 이미 `ctx.Response` 를 받으므로 신규 fetch 없음.

```go
// 신규 빌더: 소스 교환 컨텍스트 + 1개 item.
//   targetURL = finding identity(=페이지 URL, 불변) — evidence 범위 밖 변경 회피
//   src       = 매치가 실제로 나온 HAREntry (F1: 페이지 body면 ctx.Response, JS면 그 js)
func findingAt(cwe domain.CWE, sev domain.Severity, targetURL string, src *domain.HAREntry, item domain.EvidenceItem) domain.Finding
// src → Evidence.Request{Method:"GET", URL:src.URL} + Evidence.Response{Status,ContentType,Headers(allowlist bounded),Truncated}
// item → Evidence.Items[0]. src.URL != targetURL 이면 호출부가 item.Location.SourceURL=src.URL 설정.
```
**(F1)** JS-소스 매치: 호출부는 `src = js`(발견된 JS HAREntry)를 넘기고 `Location.SourceURL=js.URL`. 그러면 Request/Response가 **JS 교환**을 기술 → AI가 올바른 재현 대상(js.URL)을 얻음. `finding()`(단순 3-인자) 헬퍼는 제거하고 전 시그니처를 `findingAt` 로 이관.

- **바디 매치**: `matchContext` 를 `(snippet string, offset, matchLen int)` 반환형으로 확장(`re.FindIndex` 결과 보존). `Location{Part:PartBody, Offset, MatchLen}`.
- **헤더 매치**(없음 — observation은 주로 바디/쿠키/URL): 해당 시 `Location{Part:PartHeader, Name:<헤더명>}`, snippet=헤더값.
- **쿠키 매치**(PS-015/016/017): `Location{Part:PartCookie, Name:<쿠키명>}`, snippet=쿠키 속성 요약(예: `"sessionid; Secure=false"`).
- **URL 매치**(PS-018 session-in-URL): `Location{Part:PartURL}`, snippet=매치 파라미터.
- **DiscoveredJS 매치**(PS-009 sensitiveComment 등): item 이 주 응답이 아닌 JS에서 오면 `Location.SourceURL = <js.URL>`; Request/Response 는 주 응답 유지.
- **응답 헤더 선별**(F3, 결정적 allowlist): `Response.Headers` 는 고정 allowlist만 담아 비결정성/발산·과다수집 제거 — `Content-Type, Server, X-Powered-By, Location, WWW-Authenticate, Content-Length` + **헤더-Part 매치를 유발한 헤더**(있으면). `MaxEvidenceHeaders` 상한, 값 `maxHeaderValueLen` 절단. allowlist 외 헤더 생략.

### Rule / Hypothesis 표 (관측 13)
| PS | CWE | Rule id | Hypothesis(가설, AI용) |
|---|---|---|---|
| 003 | CWE-311 | `observation:CWE-311/plaintext-login` | HTTP(비-TLS) 페이지에 로그인 폼 → 자격증명 평문 전송 |
| 008 | CWE-651 | `observation:CWE-651/robots-sensitive` | robots.txt가 민감 경로 노출 |
| 009 | CWE-615 | `observation:CWE-615/<gitleaks-rule>` | 주석/스크립트에 시크릿/토큰 하드코딩 (config 룰 id 전파) |
| 010 | CWE-527 | `observation:CWE-527/git-exposure` | `.git` 메타데이터 노출 |
| 011 | CWE-538 | `observation:CWE-538/env-exposure` | `.env` 등 설정 파일 노출 |
| 012 | CWE-530 | `observation:CWE-530/backup-file` | 백업/아카이브 파일 노출 |
| 013 | CWE-306 | `observation:CWE-306/admin-open`·`/api-docs` | 인증 없는 관리/문서 표면 |
| 018 | CWE-598 | `observation:CWE-598/session-in-url` | URL 쿼리에 세션 토큰 |
| 022 | CWE-942 | `observation:CWE-942/crossdomain` | 과대허용 crossdomain.xml |
| 023 | CWE-799 | `observation:CWE-799/captcha-absence` | 로그인에 캡차 부재(무차별 대입) |
| 015 | CWE-614 | `observation:CWE-614/cookie-secure` | 쿠키 Secure 미설정 |
| 016 | CWE-1004 | `observation:CWE-1004/cookie-httponly` | 쿠키 HttpOnly 미설정 |
| 017 | CWE-1275 | `observation:CWE-1275/cookie-samesite` | 쿠키 SameSite 미설정 |

(Hypothesis 문자열은 짧게, CWE 근거 요약. config-driven(009/018/013)은 룰별 세부 id를 Rule에 전파.)

---

## 3. FD-3 — probe 캡처 (best-effort·정직, `probe/parsers.go`)

`pFinding()` 는 외부 CLI stdout만 있으므로 채울 수 있는 것만:

```go
func pFinding(cwe domain.CWE, tool, url, evidence string) domain.Finding
// → Evidence.Items[0] = {Snippet: clip(evidence), Rule: tool, Hypothesis: hypothesisFor(cwe)}
// → Location / Request / Response = 생략(nil/zero). 관측 안 한 것을 관측한 척 금지(FR-E7).
```

- `Rule = tool`(예: `humble`, `testssl`) — 귀속.
- `Hypothesis = hypothesisFor(cwe)` — 소유 CWE별 짧은 가설(관측표와 톤 일치).
- **Location/Request/Response 는 날조하지 않음** — probe는 HAREntry 없음. omitempty로 직렬화에서 빠짐.

---

## 4. FD-4 — normalize (dormant, 컴파일+정직, `normalize.go`)

live 미배선이나 컴파일 필요. `RawFinding.Evidence []string` → `[]EvidenceItem{Snippet}`:
```go
Evidence: domain.Evidence{Items: boundItems(rawItems(raw.Evidence), n.maxItems)}
```
- 각 raw 문자열 → `EvidenceItem{Snippet: s}`(위치/룰 없음 — pre-structured 경로의 정직한 최소형).
- `maxSnippets` → `maxItems`(=MaxEvidenceItems) 개명.

---

## 5. FD-5 — dedup collapse (`dedup.go`)

```go
func (d *deduplicator) collapse(a, b domain.Finding) domain.Finding {
    a.Severity = domain.Max(a.Severity, b.Severity)
    a.Evidence.Items = boundUnionItems(a.Evidence.Items, b.Evidence.Items, d.maxItems) // item identity로 union, 상한
    if a.Evidence.Request == nil  { a.Evidence.Request = b.Evidence.Request }   // 동일 identity → 동일 교환, 최초 non-nil
    if a.Evidence.Response == nil { a.Evidence.Response = b.Evidence.Response }
    return a
}
```
- **item identity**(union dedup 키) = `(Snippet, Location.Part, Location.Name, Location.Offset, Location.SourceURL)`. 결정적·멱등(P2 유지).
- Request/Response는 같은 IdentityKey(=CWEs+TargetURL+SourceTool)면 동일 교환이므로 최초값 유지가 안전.

---

## 6. FD-6 — 영속성 (seal/open 전체 구조, store + taskstore)

- `store_mongo.sealEvidence`: `if ev.Empty() { return zero }` else `json.Marshal(ev)`(**전체 구조**) → `crypto.Seal(…, EvidenceAAD)`. `openEvidence`: `json.Unmarshal(pt, &ev)`(전체). AAD 바인딩 불변.
- **(F4, resiliency)** 무마이그레이션(D4)이라 배포 후 **구 리포트**의 평문은 구 스키마(`["s1","s2"]` JSON 배열)라 신 구조(객체)로 Unmarshal 실패. openEvidence는 이때 **하드 에러 대신 빈 Evidence + 진단 로그로 강등**(finding·리포트 전체 읽기를 실패시키지 않음). taskstore ledger open도 동일. (신규 스캔은 정상 왕복.)
- `taskstore_mongo`: ledger seal/open 동일하게 `f.Evidence`(전체) 직렬화, `LedgerEvidenceAAD` 불변. 게이트는 `f.Evidence.Empty()`.
- `taskstore.PurgeOutcomeEvidence`(in-mem): `Findings[i].Evidence = domain.Evidence{}`(현행, zero값 — 그대로).
- `store.keepExistingEvidence`(in-mem never-erase): `len(f.Evidence.Items) > 0` 기준으로 보존/복원(현행 `Snippets` → `Items`).
- **정직성/회귀 방지**(과거 r17 F1 유형): seal→open 왕복이 전체 구조를 온전히 보존해야 함 → PBT 대상(FD-9).

---

## 7. FD-7 — 리포트 노출 (`report_view.go` + `report_ui.go`)

`findingDTO.Evidence []string` → 구조화 DTO:
```go
type evidenceItemDTO struct {
    Snippet    string       `json:"snippet"`
    Location   *locationDTO `json:"location,omitempty"`
    Rule       string       `json:"rule,omitempty"`
    Hypothesis string       `json:"hypothesis,omitempty"`
}
type findingDTO struct {
    …기존…
    Evidence []evidenceItemDTO `json:"evidence,omitempty"`
    Request  *requestDTO       `json:"request,omitempty"`
    Response *responseDTO      `json:"response,omitempty"`
}
```
- 고객 요약 카운트/파티션/result 파생 **불변**(FR-E12).
- **`report_ui.go`(HTML/JS)**: 최소 갱신 — 각 item을 `snippet` + 작은 배지(`rule`, `location.part`)로 렌더(주 소비자는 JSON). 기존 `(f.evidence||[]).forEach(ev => <pre>ev</pre>)` → `ev.snippet` 사용하도록 교정(안 하면 `[object Object]` 렌더).

---

## 8. FD-8 — 보안·redaction (security-baseline, blocking)

- **경계**(FR-E8): Items ≤ `MaxEvidenceItems`, snippet ≤ `maxEvidenceLen`, 헤더 리스트 ≤ `MaxEvidenceHeaders`, 헤더값 ≤ `maxHeaderValueLen`.
- **at-rest 암호화**(FR-E9): 전체 Evidence가 `crypto.Envelope`로 봉인(FD-6). 평문은 실행 중 메모리에만.
- **민감정보**(FR-E10, 승인됨): 바디 snippet의 시크릿(PS-009)·URL 세션 토큰(PS-018)은 AI 정오탐에 **필요 → 보존**(암호화된 채). 로그 출력 시 redact(현행 SECURITY-03 유지). **요청 헤더는 미기록(D5)** 이라 Authorization/Cookie 유출 경로 없음. 응답 헤더 선별은 관련 헤더만 담아 과다수집 회피.
- **결정 D5**: `RequestContext.Headers` 는 현재 fetcher가 전송 헤더를 기록하지 않으므로 **생략**(Method+URL이 passive GET 재현의 핵심). 날조 대신 정직한 생략. 향후 fetcher가 전송 헤더 기록 시 채움(소규모 후속).

---

## 9. FD-9 — 불변 · PBT (property-based-testing FULL, blocking)

**불변**(FR-E13): 26-파티션·checker↔CWE 소유권·probe 정직성(unwired→tool-failed)·안전봉투·제품 Dockerfile·platform seam 인터페이스.

**PBT properties**:
1. **경계**: 임의 finding 집합 → dedup 후 모든 finding의 `len(Items) ≤ MaxEvidenceItems`, 모든 snippet ≤ maxEvidenceLen, 헤더 리스트/값 ≤ 상한.
2. **dedup 멱등·결정적**(P2 유지): `Dedup∘Dedup = Dedup`, 순서 무관.
3. **probe 날조 없음**: probe 경로 evidence item은 Location/Request/Response 가 zero/nil(관측 안 한 필드 미생성).
4. **seal 왕복 = 항등**: 임의 Evidence → `open(seal(e)) == e`(store + taskstore AAD 경로 각각). F2 교정된 `Empty()`(Items+Request+Response 모두 빔) 덕에 **무조건** 성립(컨텍스트만 있는 evidence도 보존).
5. (기존 유지) 26-파티션·소유권 e2e.

---

## 10. 영향 파일 (Code Gen 대상)

`internal/domain/finding.go`(모델·상수·Empty) · `observation/{signatures.go, signatures_config.go, wellknown.go, cookie 관련}`(캡처) · `probe/parsers.go`(best-effort) · `findings/normalize/normalize.go` · `findings/dedup/dedup.go` · `findings/store/{store.go, store_mongo.go}` · `platform/taskstore/taskstore_mongo.go` · `platform/api/{report_view.go, report_ui.go}` + 각 회귀/PBT 테스트.

**신규 의존성**: 없음(표준 라이브러리).

---

## 11.5 자기검증 (근거 추출 → 요구사항 대비 필터 → 디자인 리뷰)

근거 R1~R12 추출 후 요구사항 대비 필터링: R5=오류, R7=불완전, R9=미명세로 판정. 나머지(R1·R2·R3·R4·R6·R8·R10·R11·R12) 유효. 세 결함 근거로 디자인 재리뷰 → 확정 결함 반영:

| 결함 | 심각도 | 반영 |
|---|---|---|
| **F1** JS-소스 finding의 Request/Response 오귀속(signatures.go:136-141 반증) | 🔴 High | FD-1 NOTE + FD-2 `findingAt(…, src)` — 소스 교환으로 Request/Response 채움 + `Location.SourceURL`. TargetURL 불변. |
| **F2** `Empty()=Items만` → seal 왕복 property 위반 | 🟠 Med | FD-1 `Empty()`에 Request/Response 포함 → 항등 무조건 성립. |
| **F3** Response.Headers "관련(예:)" 미명세 | 🟠 Med | FD-2 결정적 allowlist + 상한. |
| **F4** 구 리포트 evidence Unmarshal 실패(무마이그레이션) | 🟠 Med | FD-6 open 실패 시 빈 Evidence+로그로 강등. |
| **F5** `MaxEvidenceSnippets→Items` 개명 churn | ⚪ Low | 개명 유지(가독성), 의미 동일 — 아래 게이트에서 선호 확인 가능. |

**긍정 확인**: dedup Request/Response "최초 non-nil"은 IdentityKey가 TargetURL 포함 → 같은 키=같은 교환이라 안전(유효 근거).

## 11. 승인 게이트

Functional Design 완료. 다음 중 선택해 주세요:

- **Request Changes** — 조정할 설계 항목(모델 형상·Rule/Hypothesis·D5 요청헤더·경계값 등)을 알려주시면 반영
- **Continue to Next Stage** — Code Generation(Part 1 계획)으로 진행
