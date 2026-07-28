# Code Generation Plan — 검출 정합성 확대 (r9, #0d 나머지)

**Unit**: scan (probe) · **Increment**: r9 · **Branch**: `feat/probe-parser-fidelity`
**Design**: `construction/scan/infrastructure-design/vuln-fixture-design.md` (approved)
**Type**: Brownfield — 기존 `internal/scan/checking/probe/` 수정 + 신규 `test/fixtures/vuln-target/`.
**이 계획이 Code Generation의 단일 소스입니다.**

## Unit Context / Traceability
- **PS-002** (testssl · CWE-1327 weak TLS) — 외부 badssl 실측
- **PS-004** (dirhunt · CWE-548 dir listing) — 로컬 fixture autoindex 실측
- **PS-025** (subzy · takeover · NoCanonicalCWE) — 로컬 mock body-only 지문 실측
- 부수: humble/httpx/Corsy(PS-001/024·007/020·021) 골든 hermetic 재캡처(외부 의존 제거)
- Backlog: 🔴 probe wiring 잔여 · production-readiness #0d(partial). 인터페이스·제품 배포 **불변**.
- 정직성 불변식: 도구 미실행/미배선/malformed → tool-failed(≠ empty-clean). 26 파티션 유지.

## 실행 전제 (환경)
- Docker 29.1.1 + compose v2.40.3 확인됨. Go 1.26.5.
- 실측 경로: chiller probe 이미지 빌드 → fixture compose 기동 → `vulnnet` 조인 컨테이너에서 probe 실행 → 실 출력 캡처.
- **badssl(testssl)은 컨테이너 아웃바운드 필요** — egress 차단 시 testssl positive는 **정직 이월**(골든/파서는 실 스키마 확인까지 시도, 불가 시 hand-authored 유지 명기).
- **정직성 원칙(F6)**: 실 도구가 실제로 finding을 내지 못하면 positive 통합/골든은 이월하고 문서에 명기 — 파서를 느슨하게 고쳐 숨기지 않음.

---

## Part 2 — Generation Steps

- [x] **Step 1 — fixture 스캐폴딩**: `test/fixtures/vuln-target/docker-compose.yml` (2 서비스 `vulnweb`/`takeover-mock`, 내부 `vulnnet`, 호스트 포트 미노출) + `vulnweb/default.conf`(nginx: server_tokens on·ACAO:*·`/listing/` autoindex·보안헤더 무) + `vulnweb/html/`(dormant 표면: robots.txt·.env(더미)·.git/config·crossdomain.xml·backup/db.sql.bak·comments.html·insecure-cookie·`/go?sid=` + listing 더미 파일) + `takeover-mock` 설정(body-only 지문 반환).
- [x] **Step 2 — fixture README + security 주석**: `test/fixtures/vuln-target/README.md` — 목적·표면 목록·기동법(`docker compose up -d`)·**security 경고**(의도적 취약·test 전용·호스트 미노출·제품 미포함).
- [x] **Step 3 — subzy 지문 확정(실측)**: 이미지 내 subzy v1.1.0 임베디드 fingerprints에서 **CNAME 불요·body-only** 지문 하나 선정 → `takeover-mock`가 그 body 반환하도록 확정. subzy를 mock 대상으로 실행해 `vulnerable=true` 재현 확인. 실패 시 → subzy positive 이월(골든만).
- [x] **Step 4 — 실 골든 캡처**: fixture(+badssl) 대상 실 probe 실행 출력으로 `internal/scan/checking/probe/testdata/` 갱신 — `dirhunt.json`·`subzy.json`·`testssl.json`(badssl 가능 시) + `humble.json`·`httpx.json`·`corsy.json` hermetic 재캡처. 캡처 불가분은 기존 골든 유지 + 사유 명기.
- [x] **Step 5 — 파서 검증·교정**: `parsers.go`의 `testsslParse`/`dirhuntParse`/`subzyParse`를 실 스키마 대비 검증. drift 시 파서+골든 교정(느슨화 금지). 인터페이스 불변.
- [x] **Step 6 — 유닛 테스트(positive + PBT)**: `parsers_test.go`에 testssl(CWE-1327)·dirhunt(CWE-548)·subzy(NoCanonicalCWE) **positive 단언** + clean/empty(표면 없음) 단언 추가/갱신. **PBT-D1**: 파서 property(산출 CWE는 owned 집합 ⊆ · malformed JSON→error · 표면 없음→empty)를 `parsers_pbt_test.go`(또는 기존 PBT 파일)로.
- [x] **Step 7 — 통합 테스트(하이브리드, IQ2=A)**: `integration_test.go` — humble/httpx/Corsy httptest positive **유지**. `TestIntegration_HostProbes_*`를 **fixture env-gated**(`PROBE_FIXTURE`/서비스명)로 확장해 dirhunt/subzy positive 단언(미설정 시 skip). testssl은 badssl/`PROBE_TEST_HOST` gated.
- [x] **Step 8 — observation dormant 명시**: fixture에 표면은 있으나 현재 finding 미발생임을 테스트 주석/요약에 정직하게 문서화(well-known provider 미배선 — 별개 backlog). (엔진 코드 변경 없음.)
- [x] **Step 9 — CI(opt-in)**: `.github/workflows/ci.yml` `probe-integration` 잡에 fixture compose 기동을 **opt-in 단계**로(기본 green 유지, IQ2=A). 또는 문서로만 명시 — 무거우면 문서화 택.
- [x] **Step 10 — 요약 문서**: `construction/scan/code/detection-fidelity-summary.md` — 무엇을 실측/이월했는지, 골든 출처, 정직성 상태, 카운트 불변 확인.
- [x] **Step 11 — 로컬 gates**: `go build ./...`·`go vet ./...`(+`-tags integration`)·`gofmt -l`·`make arch-check`·`go test ./... -race`·`gosec`(0). green 확인.

## Deliverables
- 신규: `test/fixtures/vuln-target/**` (compose·nginx conf·정적 표면·mock·README).
- 수정: `internal/scan/checking/probe/parsers.go`(검증/교정) · `testdata/*.json`(실 골든) · `parsers_test.go`·`integration_test.go`(+PBT) · (opt-in) `.github/workflows/ci.yml`.
- 문서: `construction/scan/code/detection-fidelity-summary.md`.
- **불변**: 제품 `Dockerfile`·`cmd/chiller`·probe adapter 인터페이스·배포 매니페스트·26 파티션.

## Success Criteria
- dirhunt/subzy(+가능 시 testssl) 파서가 실 취약 출력에서 올바른 CWE positive 추출(유닛 골든 + env-gated 통합).
- fixture 커밋(격리·미노출), humble/httpx/Corsy hermetic 재검증.
- observation dormant 표면 정직 문서화. 정직성 불변·26 파티션·인터페이스 불변.
- 로컬 gates green. 실측 불가분(badssl egress 등)은 정직 이월 명기.
