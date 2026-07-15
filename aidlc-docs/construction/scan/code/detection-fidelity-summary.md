# Code Summary — 검출 정합성 확대 (r9, #0d 나머지)

**Increment**: r9 · **Branch**: `feat/probe-parser-fidelity` · **Date**: 2026-07-15
**Goal**: testssl(PS-002)/dirhunt(PS-004)/subzy(PS-025) positive-finding 파서를 **실 취약 출력**으로 실측·고정하고, 재사용 통합 취약 fixture를 커밋해 검출 정합성을 넓힌다.

## 실측 결과 — 실 도구 출력으로 검증/교정

| Probe | PS · CWE | 실 스키마 검증 결과 | positive 실측 |
|---|---|---|---|
| **testssl.sh** | PS-002 · CWE-1327 | 스키마 **정상**(flat array `[{id,severity,finding}]`). 파서 교정 불요. | ✅ rc4.badssl.com `cipherlist_LOW` **HIGH** → CWE-1327 |
| **dirhunt** | PS-004 · CWE-548 | 스키마 **틀림→재작성**: 실제는 `index_of_processors[].crawler_url.flags:"index_of"` (기존 가정 `results[].flags:"directory-listing"`은 날조 — 실 출력에 한 번도 매칭 안 됨). `url.address` string/object 다형성 처리 추가. | ✅ nginx autoindex → CWE-548 |
| **subzy** | PS-025 · takeover | 스키마 **틀림→재작성**: 실제 취약 신호는 문자열 `status:"vulnerable"` (기존 가정 boolean `vulnerable`는 실 출력에 없음 → 진짜 취약도 silent clean이었음). | ✅ takeover 지문 mock → NoCanonicalCWE |
| humble | PS-001/024 | 정상(r8). fixture로 hermetic 재캡처. | ✅ 보안헤더 무 → CWE-319 + CWE-1021 |
| httpx | PS-007/020 | 정상(r8). fixture로 재캡처. | ✅ 버전배너 → CWE-693 |
| Corsy | PS-021 | 정상(r8). fixture로 재캡처. | ✅ wildcard ACAO → CWE-346 |

**두 개의 실 파서 버그를 #0d가 잡음**: dirhunt·subzy는 이전까지 실 취약 출력에도 finding을 **한 번도 못 냈다**(날조 스키마). 이제 실 골든 대비 재작성 + PBT로 고정.

## 변경 파일
- **수정** `internal/scan/checking/probe/parsers.go` — `dirhuntParse`/`dirhuntReport` 재작성(index_of_processors + 다형성 address `dirhuntURLField`), `subzyParse`/`subzyEntry` 재작성(status 문자열), 헤더 F6 주석을 "verified"로 갱신. testssl/humble/httpx/corsy 파서 로직 불변.
- **수정** `internal/scan/checking/probe/testdata/*.json` — 6개 골든 전부 실 캡처로 교체(testssl=rc4.badssl.com, dirhunt/subzy/humble/httpx/corsy=fixture).
- **수정** `internal/scan/checking/probe/parsers_test.go` — humble positive를 fixture 기준(CWE-319+1021 2건)으로 갱신. 나머지 positive 단언 유지.
- **신규** `internal/scan/checking/probe/parsers_pbt_test.go` — PBT-D1(owned-CWE만 산출·valid severity·tool명·evidence bound) 300 iter, dirhunt/subzy/testssl.
- **수정** `internal/scan/checking/probe/integration_test.go` — `TestIntegration_FixtureProbes_PositiveFindings`(dirhunt/subzy/testssl env-gated positive) 추가. 기존 URL-probes httptest positive 유지(하이브리드, IQ2=A).
- **신규** `test/fixtures/vuln-target/**` — docker-compose(vulnweb nginx + takeover-mock, 내부 `vulnnet`, 호스트 미노출) + nginx conf + dormant observation 정적 표면 + README(security 경고).
- **수정** `.github/workflows/ci.yml` — opt-in `probe-fixture-positive` 잡(`workflow_dispatch`) + fixture 기동/subzy DB fetch/in-image positive/teardown. 기본 CI 무영향.

## 검증 (로컬 + in-image)
- 로컬 gates green: `go build`·`go vet`(+`-tags integration`)·`gofmt -l`(clean)·`make arch-check`(OK)·`go test ./... -race`(green)·`make gosec`(0).
- **in-image end-to-end positive (실제 어댑터 defaultExec→파서 경로)**: `TestIntegration_FixtureProbes_PositiveFindings` 전부 PASS — dirhunt CWE-548 · subzy takeover · testssl CWE-1327(rc4.badssl.com) + URL-probes(humble/httpx/corsy) PASS.

## 정직성·불변 (유지)
- **정직성 불변식 유지**: 도구 미실행/미배선/malformed → tool-failed(≠ empty-clean). 파서를 느슨하게 고쳐 스키마 drift를 숨기지 않음(실 골든 대비 교정).
- **26 파티션·소유권 불변**: 이번 증분은 ownership을 옮기지 않음(cookie 이전은 r8). probe-owned 9 / observation 13 유지. registry·test wiring 카운트 테스트 green.
- **인터페이스·제품 배포 불변**: adapter 인터페이스·`cmd/chiller`·제품 `Dockerfile`·배포 매니페스트 변경 없음. 신규는 전부 `test/` 스캐폴딩.

## observation dormant 표면 (C5=B, 정직 명기)
fixture는 robots.txt·`/.env`(더미)·`/.git/config`·`/crossdomain.xml`·`/backup/db.sql.bak`·민감 주석·insecure Set-Cookie·session-in-URL 표면을 포함하나, **현재 엔진이 well-known-path GET을 하지 않아 finding이 나지 않는다**(별개 backlog: 🟠 HIGH observation well-known provider 미배선). 표면은 그 증분에서 재사용하도록 심어둠. fixture README에 명시.

## 정직한 이월 / 후속 (F6)
- **subzy fingerprints DB 운영 갭**: subzy v1.1.0의 자체 fetch URL이 죽어(404 텍스트를 파싱→에러) DB 없이는 tool-failed. 제품 이미지에 DB를 번들하지 않음(이번 스코프=파서 정합성, Dockerfile 불변). **파서는 실 취약 출력으로 검증 완료**; 런타임 DB provisioning(이미지에 fingerprints.json 번들 or 사이드카)은 후속 항목 → production-readiness에 기록. 정직성상 현재 prod subzy는 tool-failed(false-clean 아님).
- testssl positive는 외부 badssl 의존(egress 필요) — 오프라인/CI 기본에서는 skip, opt-in 잡에서만 실행.
