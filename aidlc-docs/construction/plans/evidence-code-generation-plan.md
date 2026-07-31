# Code Generation Plan — Evidence 강화 (r20)

**Stage**: Code Generation Part 1 (Planning) · **Branch**: feat/evidence-enrichment
**FD**: `construction/scan/functional-design/evidence-functional-design.md` (approved, F1~F4 교정 포함)
**Status**: ✅ CODE COMPLETE + 전 게이트 GREEN 2026-07-31 (kernel-panic 후 재개·검증). 승인 게이트 대기.

## 전략
`domain.Evidence` 교체가 전 소비자 컴파일을 깬다 → **domain 먼저**, 그 위에서 소비자를 컴파일 복구하며 강화. 순서: model → findings(normalize/dedup/store) → platform(taskstore/api) → scan capture(observation/probe) → 테스트/PBT → 게이트. 각 스텝 끝에 부분 빌드로 조기 검증.

**불변(전 스텝 공통)**: 26-파티션·checker↔CWE 소유권·probe 정직성(unwired→tool-failed)·안전봉투·제품 Dockerfile·platform seam 인터페이스. 신규 의존성 0.

---

## Step A — 도메인 모델 (`internal/domain/finding.go`)
- [x] A1. `Evidence` 교체: `Items []EvidenceItem` + `Request *RequestContext` + `Response *ResponseContext` (bson/json 태그, omitempty).
- [x] A2. 신규 타입: `EvidenceItem{Snippet,Location,Rule,Hypothesis}` · `Location{Part,Name,Offset,MatchLen,SourceURL}` · `MatchPart`(+PartHeader/Body/Cookie/URL 상수) · `RequestContext{Method,URL,Headers}` · `ResponseContext{Status,ContentType,Headers,Truncated}`.
- [x] A3. 상수: `MaxEvidenceSnippets`→`MaxEvidenceItems`(=8, 전 참조 개명) · 신규 `MaxEvidenceHeaders`(=12) · `maxHeaderValueLen`(=256).
- [x] A4. `func (e Evidence) Empty() bool`(F2: Items+Request+Response 모두 빔). `Validate()`는 evidence 미검증 유지.
- **수용**: `go build ./internal/domain/` green(소비자는 아직 깨짐 — 예상).

## Step B — findings normalize + dedup
- [x] B1. `normalize.go`(dormant): `RawFinding.Evidence []string` → `Evidence{Items: boundItems(rawItems, maxItems)}`(각 문자열→`EvidenceItem{Snippet}`). `maxSnippets`→`maxItems`.
- [x] B2. `dedup.go`: `collapse`가 `boundUnionItems`(item identity=`(Snippet,Part,Name,Offset,SourceURL)`로 union, 상한) + Request/Response 최초 non-nil. `boundUnion`(문자열) 제거/교체.
- **수용**: `go build ./internal/findings/normalize/ ./internal/findings/dedup/` green.

## Step C — findings store
- [x] C1. `store.go`(in-mem never-erase): `keepExistingEvidence`가 `len(f.Evidence.Items)>0` 기준으로 보존/복원.
- [x] C2. `store_mongo.go`: `sealEvidence`=`if ev.Empty(){zero} else json.Marshal(ev)`(전체) → Seal(EvidenceAAD). `openEvidence`=Unmarshal 전체 + **F4 강등**(Unmarshal 실패 시 빈 Evidence+`log`, 에러 아님).
- **수용**: `go build ./internal/findings/...` green.

## Step D — platform taskstore
- [x] D1. `taskstore_mongo.go`: ledger seal/open을 `f.Evidence`(전체) 직렬화, `LedgerEvidenceAAD` 불변, `Empty()` 게이트 + **F4 강등**.
- [x] D2. `taskstore.go`(in-mem `PurgeOutcomeEvidence`): `domain.Evidence{}` zero화 — 현행 유지 확인.
- **수용**: `go build ./internal/platform/taskstore/` green.

## Step E — observation 캡처 (완전 구조화) — 최대 스텝
- [x] E1. `signatures.go`: `finding()`(3-인자) 제거 → `findingAt(cwe,sev,targetURL,src *HAREntry,item)` 도입. `respContext(src)`(Request{GET,src.URL} + Response{Status,ContentType,Headers=allowlist(F3),Truncated}) 헬퍼. **PD-B: `findingAt`은 `src=nil` 을 견딤**(sessionInURL은 ctx.Response 가드 없이 URL만으로 발화) → nil이면 `Request{GET,targetURL}`·`Response=nil`.
- [x] E2. `matchContext`를 `(snippet string, offset,matchLen int)` 반환형으로 확장(`FindIndex` 보존). 바디 매치→`Location{Part:PartBody,Offset,MatchLen}`.
- [x] E3. 시그니처별 Location/Rule/Hypothesis(FD-2 표 13개): plaintextLogin·robots·gitExposure·envExposure·backup·crossdomain·captcha(바디/경로) · session-in-URL(`Part:PartURL`) · cookie 3종(`Part:PartCookie,Name`) · sensitiveComment/admin/api-docs(config-driven, 룰 세부 id 전파).
- [x] E4. **F1**: `sensitiveCommentSig`의 JS 경로가 `src=js` 넘기고 `Location.SourceURL=js.URL`. 페이지 경로는 `src=ctx.Response`.
- [x] E5. **PD-A: config-룰 매치 헬퍼 확장** — `sensitiveComment.scan()`·`APIDocRules.match()`·`InternalRules.match()`가 현재 문자열만 반환·오프셋 폐기 → `(ruleName string, snippet string, offset,matchLen int)` 반환으로 넓혀 `Location{Part:PartBody,Offset,MatchLen}` + `Rule=observation:CWE-…/<ruleName>` 채움. `SecretRule.Name` 재사용(config 재생성 불요). `signatures_config.go`·`wellknown.go`: well-known 경로 매치 Location(`Part:PartBody`/경로) + Rule/Hypothesis 전파.
- **수용**: `go build ./internal/scan/checking/observation/` green.

## Step F — probe 캡처 (best-effort·정직)
- [x] F1. `parsers.go`: `pFinding`이 `Evidence{Items:[{Snippet:clip(ev),Rule:tool,Hypothesis:hypothesisFor(cwe)}]}`. Location/Request/Response 미설정(nil/zero, omitempty).
- [x] F2. `hypothesisFor(cwe)` 소유 CWE별 짧은 가설.
- **수용**: `go build ./internal/scan/checking/probe/` green.

## Step G — 리포트 노출
- [x] G1. `report_view.go`: `findingDTO.Evidence []evidenceItemDTO` + `Request/Response` DTO. join에서 `f.Evidence.Items`/`Request`/`Response` 매핑. 요약 카운트/파티션 불변.
- [x] G2. `report_ui.go`(JS): 드릴다운이 `ev.snippet` 렌더(R10 버그 수정) + `rule`/`location.part` 작은 배지. probe(위치 없음) graceful.
- **수용**: `go build ./...` green.

## Step H — 테스트 + PBT (FULL, blocking)
- [x] H1. 회귀: 기존 evidence 단언 테스트(evidence_test/cookie_test/distilled_sig_test/parsers_test/report) 신 구조로 갱신 + Location/Rule/Hypothesis 단언 추가.
- [x] H2. **PBT-경계**: dedup 후 `len(Items)≤MaxEvidenceItems`, snippet≤maxEvidenceLen, 헤더 리스트/값≤상한.
- [x] H3. **PBT-멱등**: `Dedup∘Dedup=Dedup`, 순서 무관(P2 유지).
- [x] H4. **PBT-probe 날조없음**: probe 경로 item의 Location/Request/Response = zero/nil.
- [x] H5. **PBT-seal 왕복 항등**: 임의 Evidence → `open(seal(e))==e` (store `EvidenceAAD` + taskstore `LedgerEvidenceAAD` 각각).
- [x] H6. **F1 회귀**: JS-소스 finding이 `Request/Response`=js 교환 + `Location.SourceURL=js.URL`.
- [x] H7. **F4 회귀**: 구 스키마(`["s"]`) 평문 open → 빈 Evidence(에러 아님).
- [x] H8. e2e: 26-파티션·소유권 불변 재확인.

## Step I — 게이트
- [x] I1. `go build ./... && go vet ./...`(+integration 태그) · `gofmt -l`(clean) · `make arch-check`.
- [x] I2. `go test ./... -race`(23 pkg green).
- [x] I3. `gosec ./...`(0) · 필요시 `make secrets`(신규 시크릿 패턴 자기매칭 없음 확인).
- **수용**: 전 게이트 green.

---

## 자기검증 (근거 추출 → 요구사항 필터 → 플랜 리뷰)
근거 P-R1~P-R9 추출 → 필터: P-R5(전 시그니처 findingAt 이관)만 불완전, 나머지 유효. 교정 결함:
- **PD-A** 🟠: config-룰 매치 헬퍼(scan/APIDocRules.match/internal.match)가 오프셋·룰명 반환 안 함 → PS-009/013/internal Location.Offset·Rule 공백. E5 명시화(헬퍼 시그니처 확장, `SecretRule.Name` 재사용).
- **PD-B** ⚪: `findingAt` src=nil 견딤(sessionInURL URL-only 발화). E1 명시.
- **de-risk**: PD4 후보 소멸 — `SecretRule.Name` 존재, config 재생성 불요.

## 승인 게이트
이 계획대로 Part 2(생성)를 실행할까요?
- **Request Changes** — 스텝/순서/범위 조정
- **Continue to Next Stage** — Part 2(코드 생성) 실행
