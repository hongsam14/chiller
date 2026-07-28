# Code Summary — Observation well-known-path GET provider + dictionary 심화 (r10)

**Increment**: r10 · **Branch**: (신규, off main) · **Date**: 2026-07-16
**Goal**: observation false-clean 해소 — approved 호스트에 well-known 경로를 GET해 소유 시그니처에 아티팩트를 전달하고, 그 6개 항목 사전을 FN 확장 + FP 축소로 심화한다.

## 무엇을 했나
- **well-known GET provider (fan-out 합성, Q1=A)**: `observation.WellKnownPaths()`(~16 고정 경로)를 `fanOut`이 distinct approved host별로 절대 Target(URL-scoped WorkItem)으로 합성. 기존 exec 경로(fetch+approval gate+per-host rate+URL-scoped signatures)를 그대로 재사용 → 소유 시그니처가 `Target.URL=well-known 경로`로 아티팩트를 받아 판정.
- **6개 항목 사전 심화 (Q4=B/C1=A/C2=A, FN 확장 + FP 축소)**:
  - **PS-011 `.env`**: 키셋 일반화(`*SECRET/PASSWORD/API_KEY/TOKEN/PRIVATE_KEY*`) + 값 실제성 가드(len≥6·비플레이스홀더·주석행 제외) → 프레임워크 `.env.example` FP 차단.
  - **PS-012 backup**: 확장자 단독 → backup-name heuristic(이중확장자/클래식 ext/backup-keyword 아카이브). `report.zip`·`data.sql` FP 차단.
  - **PS-010 `.git`**: path guard `strings.Contains("/.git")` → 정규식 `/\.git(/|$)` → `/.gitignore`·`/.github/` FP 차단. `/.git/HEAD`(`ref: refs/`) 추가.
  - **PS-013 admin**: 목록 확장(`manager/html`·`phpmyadmin`·`dashboard`·`backend` 등) + login/SPA 마커 배제(SPA false-unauth 차단).
  - **PS-008 robots**: Disallow 라인 키워드 확장(`database`·`sql`·`.env`·`logs`·`db`·`api`).
  - **PS-022 crossdomain**: 유지.

## 변경 파일
- **신규** `internal/scan/checking/observation/wellknown.go` — `WellKnownPaths()` 고정 목록.
- **수정** `internal/platform/orchestrator/fanout.go` — well-known URL-scoped WorkItem 합성(PR-W1..6). 대표 URL의 authority(host:port) 보존(포트 유실 방지). observation import(platform→scan, arch-check 통과).
- **수정** `internal/scan/checking/observation/signatures.go` — 5개 시그니처 v2 anchor(env/backup/git/admin/robots).
- **신규** `internal/scan/checking/observation/wellknown_sig_test.go` — FP/FN 코퍼스 27 케이스.
- **수정** `internal/platform/orchestrator/orchestrator_test.go` — 합성 카운트 반영 + 합성/MaxURLs-독립 테스트.
- **수정** `test/fixtures/vuln-target/` — well-known 표면 활성화/확장(`.env` 현실적 secret·`/db.sql`·`/admin`·`/.git/HEAD`).
- **신규** `test/wellknown_integration_test.go` — orchestrator 스캔 end-to-end well-known positive(env-gated).
- **수정** `test/platform_e2e_test.go` — 합성 반영(WorkerPool rate 상향·FinancialProxy atomic·slow 핸들러 경로 격리).
- **수정** `.github/workflows/ci.yml` — opt-in 잡에 well-known 통합 스텝.
- **문서** `construction/scan/code/wellknown-summary.md`.

## 검증
- 로컬 gates green: build·vet(+integration)·gofmt·arch-check·`go test ./... -race`·gosec(0).
- **FP/FN 코퍼스 27 케이스 PASS** (blocking, C2=A): 각 v2 anchor가 실 취약(FN)에 발화 + 짝 benign(FP: 프레임워크 `.env`·`report.zip`·`/.gitignore`·SPA/login admin·평범 robots)에 미발화.
- **fanOut 합성 유닛**: distinct host별 1회·URLScoped·MaxURLs 독립.
- **end-to-end (실 orchestrator 스캔, in-container on fixture net)**: 홈페이지 1개 URL 스캔이 well-known sweep으로 **6개 소유 CWE 전부 산출** — CWE-651(robots)·538(env)·527(git config+HEAD)·942(crossdomain)·530(backup)·306(admin), coverage 26 파티션 유효.

## 설계상 발견/주의
- **포트 보존**: 합성 URL은 대표 URL의 authority(host:port)를 써야 함 — bare Host는 비표준 포트를 유실해 :80을 침. 수정 반영(실 버그).
- **rate budget 공유(Q6=A/RES-W1)**: well-known ~16 요청이 **같은 호스트** per-host rate(기본 5/s)를 공유 → 호스트당 스캔 지연 +~3s. 의도된 동작(신뢰경계 유지). 대량 스캔 시 `RatePerHost` 상향으로 조절 가능.
- **Fetcher DNS**: 스캔 Fetcher(projectdiscovery retryablehttp)는 자체 resolver라 Docker 서비스명 미해석 — 실 DNS명은 정상, fixture 검증은 컨테이너 IP 사용.

## 불변 (유지)
- 정직성: well-known GET이 404/비취약이면 checked-clean(false-clean 아님); 실패는 해당 경로만 tool-failed(격리).
- **26 파티션·CWE 소유권·checker scope·인터페이스·제품 Dockerfile·probe·platform seam 불변.** 능동 크롤링 없음(고정 well-known GET만, FR-03).

## 이월 (다음 todo)
- P1 **비-well-known** dictionary: PS-009 comment·PS-018 sid·PS-023 captcha (C1=A로 이번 제외).
- P0 transport(br/zstd·512KB·SPA `hasPasswordField`) — 별개 correctness 증분(C3=A).
- 관찰: fixture가 모든 응답에 Set-Cookie를 붙여 합성 항목마다 쿠키 finding이 중복 산출(테스트 대상 아티팩트; 실 서버는 무관). 필요 시 쿠키 시그니처를 URL당 dedup하는 별도 개선 가능.
