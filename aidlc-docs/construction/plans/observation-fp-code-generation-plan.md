# Code Generation Plan — Observation 오탐 저감 (r16)

Branch: `feat/observation-fp` (off feat/compliance-report = r10+r15 포함). Part 2에서 아래 순서 실행.

## A. 시그니처 취득 파이프라인 (Arch A · Level 2 — G1)

- [ ] **A0** `third_party/<src>/` pinned 스냅샷: curl@pinned-SHA로 gitleaks(`gitleaks.toml`)·ZAP(param
      목록 + private-IP 데이터)·Nuclei(exposures/apis subset) 원본 동봉 + `LICENSE` + `SOURCE`(URL/commit).
- [ ] **A1** `tools/sigimport/`(Go, tool-스코프 dep 격리) 어댑터: gitleaks(TOML)/zap(txt)/nuclei(YAML) →
      우리 통일 `configs/signatures/*.yaml`. **RE2 비호환 = 등가변환 또는 `# skipped:` 기록**(조용한 누락 금지).
      각 출력에 provenance 헤더(source/commit/license/rules N/skipped M).
- [ ] **A2** `make signatures` 타깃(= `go run ./tools/sigimport`) + **CI drift-check**
      (`make signatures && git diff --exit-code configs/signatures/`).
- [ ] **A3** 카테고리: secrets(gitleaks 전량)·url-sensitive(ZAP params+값패턴)·api-docs(Nuclei 범용 포맷)·
      internal-disclosure(ZAP Private-IP RFC1918/EC2 + 비-prod 휴리스틱).

## B. Config 파일 (configs/signatures/, 카테고리별)

- [ ] **B1** `configs/signatures/secrets.yaml` (gitleaks 증류, 헤더에 MIT attribution + "distilled, not executed").
- [ ] **B2** `configs/signatures/url-sensitive.yaml` (ZAP, Apache attribution).
- [ ] **B3** `configs/signatures/api-docs.yaml` (Nuclei, MIT attribution).
- [ ] **B4** `configs/signatures/internal-disclosure.yaml` (ZAP Private-IP, Apache attribution).

## C. 로더 + 엔진 로직 (observation, arch-check 안전)

- [ ] **C1** `observation/signatures_config.go`: 타입(secretRule/urlRules/apiDocRule/internalRules) +
      디렉토리 스캔 로더 `LoadSignatureRules(dir) (Rules, error)` — **부팅 RE2 컴파일·fail-closed**
      (잘못된 regex/누락 → error). Shannon 엔트로피 헬퍼.
- [ ] **C2** `sensitiveCommentSig` 개조: 주입된 secrets(엔트로피 게이트+allowlist+JS-리터럴 가드) +
      internal-disclosure 룰 소비. 증적 = 매칭 실데이터(FR-4, bounded). 주석+JS만(FR-5).
- [ ] **C3** `sessionInURLSig` 개조: url-sensitive(params+valuePatterns) 소비(FR-8).
- [ ] **C4** `adminPageSig` 개조: api-docs 룰(path+bodyMarker) 전용 분기 추가(FR-9), 소유 CWE PS-013 유지.
- [ ] **C5** 엔진 구성: `observation.Default()` → 룰 주입형 `observation.New(Rules) []Signature`(또는
      `Checkers`가 Rules 받음). 기존 호출부(registry/boot/tests) 갱신.
- [ ] **C6 [G4]** 테스트 구성 전략: 테스트는 실 `configs/signatures/`를 상대경로 로드(다른 config 테스트
      관례) 또는 `testRules()` 픽스처. **소유권 룰-독립 보장**: 시그니처는 0 룰이어도 자기 CWE를 소유
      (config 문제로 26 파티션·boot gate 안 깨짐); fail-closed 로더가 누락을 기동거부로 처리.

## D. FR-6 구조 (well-known 소유-시그니처-only)

- [ ] **D1** `observation.WellKnownPaths()` → `[]WellKnownPath{Path, CWE}` (path→owning-CWE).
- [ ] **D2 [G5]** `domain.WorkItem`에 `WellKnownCWE domain.CWE`(additive, omitempty)를 **단일 진실**로
      (well-known ⟺ `WellKnownCWE != ""`). 기존 `WellKnown bool`은 메서드/파생으로 정리(desync 방지),
      사용처 exec:88/finalize:204/fanout:95 갱신.
- [ ] **D3** `orchestrator/fanout.wellKnownItems`: 항목별 `WellKnownCWE` 세팅.
- [ ] **D4** `exec`: well-known 항목 → `observationForCWE(urlScoped, cwe)`(단일 소유 시그니처). `observationOnly` 대체.
- [ ] **D5** `finalize.ranCheckers`: 동일 미러(well-known → 단일 소유 시그니처). 정직성 미러 규율 유지.
- [ ] **D6 [G6]** api-doc 경로를 well-known 스윕에 **선제 추가**(`/v3/api-docs`·`/swagger-ui`·
      `/openapi.json`·`/actuator`, owning CWE-306) → 크롤 없이 human #10 선제 포착. 스윕 크기↑(수용).

## E. 배선 + 배포

- [ ] **E1 [G3]** `cmd/chiller`: `--signatures configs/signatures`(기본) 플래그(--mapping/--coverage 선례).
      부팅 시 로드 → observation 엔진 주입, 실패 시 fail-closed. observation을 구성하는 **모든 role**
      (exec=worker/all, boot-gate ownership=manager/all)에서 로드.
- [ ] **E2** Dockerfile `COPY configs /app/configs`(78행) → signatures 자동 동봉(확인만).

## F. 검증 (Q3=A, blocking)

- [ ] **F1** FP/FN 코퍼스: minified-JS FP(`password:!0`)=0 · gitleaks 토큰 FN · JSON형 · ZAP url FN/FP ·
      Nuclei api-docs FN/FP · ZAP private-IP/비-prod FN · 기존 정탐 회귀 — 각 verdict 단언.
- [ ] **F2** ground-truth 회귀: human #3(accessToken URL)·#10(v3/api-docs)·#15(JS 실토큰 eyJ)·#16(stage-www).
- [ ] **F3** FR-6 구조 회귀: well-known이 소유 시그니처만 실행(PS-009가 /admin 등에서 미발화) + exec/fold 미러 일치.
- [ ] **F4** 로더 fail-closed 테스트(잘못된 regex/파일 부재 → 기동 거부).
- [ ] **F5** PBT: finding은 소유 CWE만, 증적 bounded.
- [ ] **F6 [G2]** 기존 e2e/통합 회귀: `e2e:215 PS-009==Checked`가 **새 룰에서도 유지**되도록 harness
      HTML-주석 픽스처를 실-시크릿 패턴으로 갱신; `TestIntegration_WellKnown_Fixture`(소유 6 CWE)·
      e2e 정직회계가 FR-6(소유-only)에서 그대로 통과함을 확인.
- [ ] **F7** 로컬 게이트: build/vet(+integration)/gofmt/arch-check/`go test -race`/gosec 0 + `make signatures` drift-check.

## 리스크/불변

Medium. 26 파티션·소유권·인터페이스·probe·seam·제품 Dockerfile 불변. observation 엔진 구성 개조(C5)가
가장 넓은 변경 — registry/boot/test 호출부 동반. FR-6는 exec/fold 정직성 미러(민감) — F3로 방어.
