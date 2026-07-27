# Code Generation Plan — Observation well-known provider + dictionary 심화 (r10)

**Unit**: scan · **Increment**: r10 · **Branch**: (신규, off main)
**Design**: `construction/scan/functional-design/wellknown-functional-design.md` (approved)
**Type**: Brownfield — `internal/scan/checking/observation/` + `internal/platform/orchestrator/fanout.go` 수정, 신규 well-known 목록. **이 계획이 단일 소스.**

## Traceability
- PS-008 robots · PS-011 .env · PS-010 .git · PS-022 crossdomain · PS-012 backup · PS-013 admin (배선 + 사전 심화)
- Backlog 🟠 HIGH observation well-known · P1 dictionary(배선 6개 한정)
- 불변: 외부 API·probe·platform seam·domain 인터페이스·26 파티션 소유권·제품 Dockerfile.

## Part 2 — Generation Steps

- [x] **Step 1 — WellKnown 경로 목록**: `internal/scan/checking/observation/wellknown.go` — `WellKnownPaths() []string` (고정 상대 경로 ~16: `/robots.txt`,`/.env`,`/.git/config`,`/.git/HEAD`,`/crossdomain.xml`, backup 5(`/backup.zip`,`/www.zip`,`/db.sql`,`/index.php.bak`,`/.env.bak`), admin 6(`/admin`,`/administrator`,`/manager/html`,`/wp-admin`,`/phpmyadmin`,`/dashboard`)). 주석: benign·고정·소수(SEC-W2).
- [x] **Step 2 — fanOut 합성 (PR-W1..6)**: `internal/platform/orchestrator/fanout.go` — `haveURL`일 때 `distinctHosts(set.URLs)` 각 호스트에 `WellKnownPaths()`를 절대 Target(`scheme://host+path`)으로 확장해 **URLScoped WorkItem** append. observation import. sequential ID 유지. (MaxURLs 제외는 구조상 보장 — ingest 후 합성.)
- [x] **Step 3 — 시그니처 v2 anchor (FN+FP)**: `internal/scan/checking/observation/signatures.go` —
  - `.env`(envAnchor v2): 키셋 일반화 + 주석행 무시 + **값 실제성 가드**(non-empty·len≥8·플레이스홀더 제외).
  - backup(backupNameAnchor v2): backup-name heuristic(이중확장자/클래식 ext/backup-keyword 아카이브); 단일 `report.zip` 미발화.
  - `.git`(gitPath v2): path guard `/\.git(/|$)`(→ `/.gitignore`·`/.github/` 배제) + HEAD `ref: refs/` 마커.
  - admin(adminPath v2): 목록 확장 + login/SPA 마커 배제.
  - robots(robotsSensitive v2): 키워드 확장(Disallow 라인 한정).
  - crossdomain: 유지. **각 시그니처 1 CWE 불변.**
- [x] **Step 4 — FP/FN 코퍼스 유닛 (PBT-W1)**: `signatures_test.go`(또는 신규 `wellknown_sig_test.go`) — 각 v2 항목의 **FN 발화**(실제 취약 body) + **FP 미발화**(프레임워크 `.env` placeholder·`report.zip`·`/.gitignore`·`/.github/`·SPA/login admin·평범 robots). + property: 산출 CWE는 소유 1개.
- [x] **Step 5 — fanOut 합성 테스트**: `fanout_test.go` — distinct host별 well-known 항목 합성·dedup·URLScoped·MaxURLs 미영향(합성이 ingest 상한과 무관) 단언. (gate/rate는 exec/fetch 레이어 기존 테스트가 커버.)
- [x] **Step 6 — r9 fixture 표면 활성화/확장**: `test/fixtures/vuln-target/` — 합성 well-known 경로가 fixture에서 **200+발화 body**를 반환하도록: `.env` 값을 현실적 secret으로(len≥8, 비플레이스홀더); `/db.sql`(또는 `/index.php.bak`) backup 파일 추가; `/admin` plain 페이지(login 마커 없음) 추가; `/.git/HEAD` 추가. README/nginx 주석 dormant→active. robots는 이미 sensitive Disallow 보유.
- [x] **Step 7 — 통합 positive (하이브리드)**: `integration_test.go` — well-known sweep positive를 fixture env-gated로 단언(PS-011 .env·PS-008 robots·PS-022 crossdomain·PS-012 backup·PS-010 .git). 기존 CI opt-in 잡 재사용/확장.
- [x] **Step 8 — 요약**: `construction/scan/code/wellknown-summary.md`.
- [x] **Step 9 — 로컬 gates**: build/vet(+integration)/gofmt/arch-check/`test -race`/gosec(0) + coverage 26 회귀(registry/test) green.

## Deliverables
- 신규: `observation/wellknown.go` · 코퍼스 테스트 · (fixture 파일 추가).
- 수정: `fanout.go`(합성) · `signatures.go`(v2 anchor) · `fanout_test.go` · fixture(`vulnweb/**`,README) · (opt-in) integration/CI.
- 문서: `construction/scan/code/wellknown-summary.md`.
- 불변: probe·platform seam·domain·외부 API·제품 Dockerfile·26 파티션 소유권.

## Success Criteria
- fixture 스캔에서 PS-008/011/012/022 real finding, PS-010 judged(.git). FP 코퍼스 미발화.
- 합성 항목이 host별 1회·URLScoped·MaxURLs 무영향. coverage 26·소유권·인터페이스 불변.
- 로컬 gates green + 통합 positive green.
