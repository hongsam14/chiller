# Production Readiness — chiller (infrastructure increment)

**Stage**: OPERATIONS (placeholder in this AI-DLC workflow — no deployment build is executed here).
**Date**: 2026-07-11. Captures the handoff checklist for the platform U1 seam-swap increment (Mongo + Valkey backends, introspection UI, deploy assets).

The code is **green** (build/vet/gofmt/arch-check/`go test ./...`/`-race`). Below is what MUST happen before the durable backends run in production, and the honest limitations carried forward.

## Increment log — PS-005/006 Nuclei 위임 복원 (r22, 2026-08-04)

**Scope:** r21이 PS-005(오류 페이지)/PS-006(스택 트레이스)를 observation-native로 승격한 것을
**정직성 관점에서 되돌림**. 수동 엔진은 벤ign GET 응답에 **이미 드러난** 에러/스택만 보고, 능동으로
**유발**하지 못하는데도 coverage가 "checked"로 표기돼 과대 주장이 됐음(코드리뷰 PLAUSIBLE #7).

**해소(회계 vs 신호 분리):** PS-005/006 → **Nuclei 위임 복원**(능동 검출 책임 명시, coverage
정직 표기). 수동 시그니처 `stackTraceSig`/`errorInfoSig`(CWE-209/497)는 **계속 등록·발화**하되
mappings에서 `{KISA: []}` 빈 투영 → **26-밖 정보노출 finding**(PII/CWE-359와 동일 패턴). 즉
"이미 드러난" 스택은 수동으로 잡아 informational로 보고하고, "유발해야 나오는" 것은 Nuclei가 소유.

**변경(config+test+docs only, 시그니처 로직 불변):** coverage/kisa.yaml(delegations 2→4:
+PS-005/006) · mappings/kisa.yaml(CWE-209/497→`{KISA:[]}`) · 주석·README. 26-파티션 회계는
r16 시절 **13 observation / 9 probe / 4 delegated**로 복귀(+ 26-밖 관측 신호 3종: 스택·에러·PII).

**전 게이트 green:** build·vet·gofmt·arch-check·`go test -race`(0)·**boot-totality(KISA+FSEC
PASS**: PS-005/006 delegated로 totality 충족, CWE-209/497 KnownCWE·Project=[])·gosec pinned(0)·
lint(0)·secrets no-leak·signatures-check drift0(시그니처 파일 무변경). 불변: 26 items·소유권·신규dep0·probe9.

## Increment log — recon 노하우 → observation 반영 (r21, 2026-08-04)

**Scope:** OffenRed-AI-DAST passive/recon 노하우를 in-process observation 엔진에 흡수 —
nuclei-templates(pinned `@9419e84`, MIT) + 큐레이트 recon 프레임을 **authoring-time에 RE2로
증류**(런타임 미사용, 신규 Go dep 0). 3개 소유 시그니처 강화 + **파티션 확장 3건**:
- **PS-012** 백업: 아카이브 매직(zip/SQLite/SQL-dump) 콘텐츠 확증(FP 축소, name 게이트 유지).
- **PS-010** `.DS_Store`(Bud1) → CWE-527 소스노출 시그니처에 편입(one-CWE-one-owner 유지).
- **PS-013** 제품노출 마커(Spring actuator/env·phpinfo·Apache server-status) + well-known 확장.
- **PS-005/006** Nuclei 위임 → **observation-native 승격**(bodyMarkerSig), delegated 4→2.
- **PII**(CWE-359, **KISA-26 밖** — 빈 투영으로 회계 0): 주민번호(YYMMDD 검증)/email/phone,
  evidence 값 마스킹(FR-5c).

**새 컨트롤/편차:** `configs/signatures/{exposures,error-disclosure}.yaml`(생성기 산출, `make
signatures-check` drift 0) · `third_party/nuclei/{exposures-product,errors-stacktrace,errors-info}` +
`third_party/offenred-recon/`(provenance). 생성기가 `part: header` 마커 스킵(죽은 규칙 방지).

**코드 리뷰(workflow high, 20 agents):** 10건 → 8 수정(maskPII 값 누출·resident-id 오탐·
compound-key 미탐·symfony 헤더 죽은규칙·/env 접미 오탐·죽은 probe·FindAllIndex·복붙 dedup) +
2 문서화 trade(**stackTrace 코드-전시 페이지 오탐** = 수동탐지 본질적 한계 · **PS-005/006 능동
트리거 상실** = Q3=B 의도, coverage Checked 설계대로).

**전 게이트 green**(build ±integration·vet·gofmt·arch-check·`go test -race` 0·gosec pinned 0·
secrets full-history no-leak·signatures-check 멱등·lint 0·observation cover 85.8%). integration/
probe-integration/trivy/syft는 CI Docker 위임.

**실 타겟 검증(`http://lab.roronoa.kr/`, in-process `--role=all`):** 부팅 게이트 OK(26 항목 전부
소유/위임) → 스캔 완료(무크래시). Coverage 26-파티션 정합: **checked 10 + N/A 5 = 15 observation
judged**, tool-failed 9(probe 바이너리 없음, 정직), **delegated 2(PS-014 ASM/PS-026 Nuclei)**.
**PS-005/006 = checked**(승격 end-to-end 확인). Findings 0(클린 홈페이지 — 실 대상 오탐 없음).
r21 신규 well-known(actuator/env·phpinfo·server-status) 스윕됨(CWE-306 12회 실행).

**이월(정직):** H2 콘솔·범용 apache server-info(pin에 nuclei 템플릿 부재) · stackTrace prose 오탐 ·
PS-005/006 능동 상실 · 발견/서브도메인 스코프(최상위 갭) · changeme(PS-019). 커밋 `5456a0c`(feat)·
`7eeb0c5`/`fb5e8ab`(docs) on feat/recon-observation. (승인됨; MVP 워크플로에 배포 빌드 없음.)

## Increment log — Evidence 강화: 구조화 스키마 (r20, 2026-07-31)

**Scope:** `domain.Evidence{Snippets []string}` → 구조화 Evidence(매치 위치 · 룰/가설 ·
요청/응답 컨텍스트)로 교체해 다운스트림 AI(능동 공격 플래너 + 정오탐 체커)가 finding당 근거를
기계 판독. observation 완전 채움, probe best-effort(날조 금지), 스키마 교체(마이그레이션 없음),
`/report` findingDTO 확장.

**Kernel-panic 복구:** 코드 생성이 다중 panic으로 중단됐으나, 재개 시 코드+테스트가 이미
완성돼 있고 전 게이트 green임을 검증으로 확인(플랜 A~I 누락 없음). 유실된 것은 추적 상태
(체크박스·audit)뿐 → 복원.

**코드 리뷰(workflow high) — 6건 수정 (data-loss 2건 포함):**
- 🔴 `taskstore_mongo.go` / 🟠 `store_mongo.go` — decrypt 성공 후 unmarshal 실패 시 evidence를
  무언 소실(resume/read data loss). **강등을 구 `[]string` 스키마로만 한정**하고 그 외 손상은
  에러로 표면화. 회귀 테스트 4개(강등 vs 손상) 추가.
- 🟠 `report_view.go` — `locationDTO.Offset` omitempty가 byte-0 매치를 wire에서 삭제 → 제거.
- 🟠 `contract_test.go` — no-alias 테스트가 `SnippetStrings()`(fresh slice) 변경으로 무의미화 →
  `Items` 직접 변경으로 deep-copy 불변 재활성.
- ⚪ `signatures.go` content-type 중복 · ⚪ `report_ui.go` 미정의 CSS 배지.

**게이트(로컬):** build/vet(+integration 태그)·gofmt·arch-check·`go test ./...`(23 pkg)·
`-race`·**gosec pinned 0**·**secrets no leaks**·coverage 56.9%. Integration 스위트(testcontainers)
는 Docker 위임(#1과 동일). PBT FULL: 경계·멱등·probe 날조없음·seal 왕복 항등 + 강등/손상 구분.

**Security-baseline(blocking) 준수:** evidence 필드별 상한(MaxEvidenceItems=8, MaxEvidenceHeaders=12,
헤더 값 클립) · at-rest 암호화(crypto.Envelope seal, AAD 바인딩 불변) · 응답 헤더 결정적 allowlist ·
요청 헤더 정직 생략(fetcher 미기록 필드는 날조 안 함).

**불변:** 26-파티션 · checker↔CWE 소유권 · probe 정직성(unwired→tool-failed) · 안전봉투 ·
제품 Dockerfile · platform seam · **신규 의존성 0**.

**커밋:** `8f0f498`(feat) · `564285b`(docs) · `071ad94`(B&T summary).
요약: `construction/build-and-test/evidence-build-and-test-summary.md`.

**남은 최상위 갭(이월):** 발견/서브도메인 스코프(사람 DAST 대비 최대 갭) · changeme/PS-019(r19 조사
종결, stub 유지 or Option C).

---

## Increment log — subzy fingerprints DB provisioning (#0e, r18, 2026-07-29)

> **CLOSED (green, Build & Test approved 2026-07-29).** Closes #0e: subzy (PS-025 subdomain takeover)
> self-fetched its fingerprint DB from a dead URL → parse error → `tool-failed` on every run. subzy v1.1.0
> has **no fingerprint-path flag** (verified via `run --help`), so the DB is provisioned **operator-side**,
> not baked into the image (it is subzy's own community-maintained data — same pattern as `make dev-env`
> secrets / storageState).
>
> - **`make subzy-db`** fetches a **pinned** `fingerprints.json` (EdOverflow/can-i-take-over-xyz @
>   `5bd4e12`) to a git-ignored `deploy/subzy/fingerprints.json`; `-f` fails loudly on a dead URL at
>   provision time (fixable), not silently at scan time. The operator mounts it read-only at
>   `/opt/subzy/fingerprints.json` (`SUBZY_DB`); the adapter runs subzy with `HOME=/opt` so it reads it.
> - **Honesty is independent of the tool:** a precheck fails PS-025 to `tool-failed` unless the exact file
>   subzy will read is a present, non-directory, **non-empty JSON fingerprint array** — an absent/empty/
>   malformed DB can never become a false-clean.
>
> **A workflow code review (high, 8 findings) followed; 6 fixed, 1 verified-no-change, 1 skipped.** The most
> severe was a real false-clean bug (the guard checked a different path than subzy reads) — fixed. Verified
> in-image (`chiller:r18`): DB mounted → subzy runs & produces a verdict; no DB → honest degradation;
> `--read-only` rootfs + DB → runs. See `construction/build-and-test/subzy-db-build-and-test-summary.md`.
>
> **PS-025 now judged** when the operator provisions the DB; only `changeme` (PS-019) remains an unwired
> stub. **Carried forward:** auto-refresh of the DB (CI cron + drift) — Q3 chose manual; a follow-up.

## Increment log — P0 Transport Correctness (r17, 2026-07-29)

> **CLOSED (green, Build & Test approved 2026-07-29).** Closes the top detection-correctness gap
> carried since r10/r16: response bodies are now **decoded by Content-Encoding** (gzip/deflate/br/zstd)
> before any checker sees them, and the 512 KiB truncation is **surfaced in the report** instead of
> read as a clean scan.
>
> - **The silent global false-negative is fixed.** Go's transport auto-decompresses only gzip (and only
>   when it implicitly asked); a `br` (Cloudflare default) / `zstd` / `deflate` response used to reach the
>   body signatures compressed, matching nothing. `internal/scan/fetch/decode.go` now streams the body
>   through the codec, **bounded on the output at 512 KiB** so a decompression bomb cannot exhaust memory.
> - **Truncation is honest.** `HAREntry.Truncated` + `WorkItemOutcome.TruncatedURLs` (the exact URLs,
>   incl. a truncated JS asset's own URL) ride the ledger; the `/report` view warns "partially scanned —
>   body truncated" so a truncated page is never read as an authoritative clean scan. A genuine transport
>   read error still fails the fetch (→ tool-failed), preserving the honesty contract.
>
> **A workflow code review (high, 10 findings) followed; 8 fixed, 1 no-change-needed, 1 skipped.** The two
> most severe were caught pre-merge: the Mongo ledger seal/open dropped the truncation field (the whole
> feature would have silently no-op'd in production Infra), and the transport was cloned per-scan (losing
> connection-pool reuse + a shared-client race). See `construction/build-and-test/transport-build-and-test-summary.md`.
>
> **New deps** (pinned): `andybalholm/brotli`, `klauspost/compress`. `make vuln`: 0 new vulns.
>
> **Carried forward (honest):** indirect `golang.org/x/text@v0.37.0` GO-2026-5970 (fix at v0.39.0+, a
> separate dep bump, not r17 scope); review F9 — the decoded HAR retains the compressed `Content-Encoding`/
> `Content-Length` headers (latent, no current consumer). **The r10/r16-carried "P0 transport correctness"
> limitation is now RESOLVED** — see limitation 3b below.

## Increment log — Observation 오탐 저감: OSS-distilled signatures + FR-6 (r16, 2026-07-29)

> **CLOSED (green, Build & Test approved 2026-07-29).** Replaced the hand-rolled observation dictionaries
> with signatures **distilled at authoring time** from pinned OSS rulesets — gitleaks (221 secret rules +
> Shannon-entropy gating + allowlists), OWASP ZAP (url-sensitive params, Private-IP internal disclosure),
> nuclei-templates (API-doc surfaces) — ported to Go RE2 and pinned under `configs/signatures/`. **Distilled,
> not executed**: chiller runs no external scanner engine. This closes the r10-carried PS-009/018/013
> dictionary gap and adds internal/infra disclosure (Gap B). A `tools/sigimport` generator regenerates the
> config from `third_party/` sources (`make signatures`), drift-checked in CI (`make signatures-check`).
>
> **FR-6 — well-known runs only its owning signature:** a synthesized well-known item now executes ONLY the
> signature that owns its path (via `domain.WorkItem.WellKnownCWE`), not the whole observation set. This
> removes the dominant false-positive amplifier (a path-agnostic PS-009 comment scan firing ~14×/host on the
> sweep — 13 of 15 findings on kbland.kr were this). `exec` and the `finalize` fold mirror the single-owner
> set, so the honest-accounting invariant is preserved.
>
> **A workflow code review (high, 7 findings) followed; 6 were fixed and 1 skipped as a documented trade** —
> see `construction/build-and-test/observation-fp-build-and-test-summary.md`. golang 철칙 (arch-check, RE2,
> fail-closed boot, honesty mirror, one-finding-per-CWE) all pass.
>
> **New operator surface:** `--signatures configs/signatures` (fail-closed boot load — a missing/invalid
> rule file refuses startup, never silently loads zero rules). Per-scan `framework` selection (KISA/FSEC)
> and a `GET /report` compliance view were also wired.
>
> **Build & Test finding (fixed):** `gitleaks detect` scans committed history, so a pre-commit `make secrets`
> misses just-added fixtures; the PS-018 test literals tripped the gate once committed → the observation
> `*_test.go` corpus is allowlisted in `.gitleaks.toml` (secret-shaped by design). Re-verified: no leaks.
>
> **Carried forward (honest, not in r16 — see aidlc-state Backlog → r16 도출 TODO):**
> - **P0 transport correctness** — `br`/`zstd`/`deflate` bodies undecoded + 512 KiB cap truncates silently.
>   Still the top detection gap: on a `br`-emitting path (Cloudflare default) every body regex sees compressed
>   bytes → global false-negative. **This is now the single largest correctness gap in the detection engine.**
> - `changeme` (PS-019) still stubbed; `subzy` needs its fingerprints DB provisioned (#0e below).
> - Review #5 (intended trade): PS-009 generic `password:`/`api_key`/`todo` anchors removed (they were a major
>   FP source); the low-entropy hardcoded-credential / dev-note tail is an accepted coverage loss.

## Increment log — Observation well-known provider (r10, 2026-07-24)

> **CLOSED (green, approved 2026-07-24).** Wired the host-scoped **well-known-path GET provider**: for each
> distinct approved host the fan-out synthesizes URL-scoped items for `/robots.txt`, `/.env`, `/.git/config`,
> `/.git/HEAD`, `/crossdomain.xml`, a pinned backup-name list, and common admin surfaces, and feeds each
> response to the owning observation signature. This closes the **6-item false-clean/N-A gap** (PS-008 robots ·
> PS-011 .env · PS-010 .git · PS-022 crossdomain · PS-012 backup · PS-013 admin) that reported "checked, no
> issue" without ever requesting the artifact. 5 signature dictionaries were deepened (FN-broaden + FP-narrow),
> corpus-asserted. A **workflow code review (high, 10 findings)** followed and **all 10 were fixed** before
> approval — see below and `construction/build-and-test/wellknown-build-and-test-summary.md`.
>
> **New operator controls (SR-1-style bound on the sweep — it sits outside `MaxURLs` by design, Q6=A):**
> - `ScanOptions.DisableWellKnown` (bool) — **off-switch**; the sweep is **on by default** (Q3=A: approved
>   hosts, approval gate + per-host rate honored).
> - `ScanOptions.MaxWellKnownHosts` (int, **default 50**) — caps distinct hosts swept; hosts beyond the cap
>   are **skipped and logged** (never silently dropped). Tune down for very large multi-host scans, or when a
>   target's WAF/IPS thresholds on `.git`/`.env` probing are a concern.
>
> **Operational notes:** the sweep shares the per-host rate budget, so expect **+~3s/host** wall-clock
> (intended). Engine-synthesized well-known items run the **observation signatures only** — the external
> probe binaries are NOT re-exec'd per synthesized path (an amplification the review caught and removed).
> The honest-accounting invariant is preserved: skipped probes are never credited as Checked.
>
> **Carried forward (honest, not in r10):** PS-009/018/023 dictionary deepening (C1=A, next todo);
> P0 transport correctness — `br`/`zstd` bodies undecoded + 512KB body cap (C3=A) — can still nullify
> even a wired check on a proxy/probe path that advertises those encodings.

## Mandatory before production (blocking)

> **#0 — MOSTLY CLOSED 2026-07-14 (probe-wiring increment r7), one Docker gate remains.**
> Six of the seven probes are now wired (argv + real JSON parser + bundled binary): humble,
> httpx, Corsy, testssl.sh, dirhunt, subzy. Only **changeme (PS-019) stays unwired** — Python2
> heritage + active credential-testing + uncertain JSON schema (F5), honestly reported
> `tool-failed`, deferred to a later increment. So on a real run chiller now judges up to
> **21 of 26** items (10 observation + 11 probe-owned; changeme's 1 + the 4 delegated remain).
> **Remaining gate below (#0a): the wiring is verified locally by unit + honesty tests, but the
> real binaries have never run — the in-image `probe-integration` job must pass once on Docker.**

| # | Action | Why | Where |
|---|---|---|---|
| ~~0~~ | ~~Wire the 7 probe CLIs~~ **DONE (6/7) 2026-07-14** — argv + JSON parser + golden fixtures per tool; multi-stage image bundles the pinned binaries; process-group kill (NFR-P1) added. changeme deferred (F5). | 12 items were `tool-failed`; now up to 11 move to judged once binaries run. Honesty invariant preserved (wired ≠ binary-present → still tool-failed until the binary runs). | `internal/scan/checking/probe/{adapters,parsers,exec_linux}.go`, `Dockerfile`; summary `construction/scan/code/probe-business-logic-summary.md` |
| ~~0a~~ | **Run the in-image `probe-integration` on Docker** — **DONE 2026-07-14.** Built the image and ran all 6 wired probes with real binaries in-image: **6/6 execute + parse clean** (URL humble/httpx/corsy; Host testssl/dirhunt/subzy vs a real host). The first run corrected the hand-authored I/O model (file-output vs stdout), tool packaging, deps (hexdump/dig), and timeouts — see `construction/scan/code/probe-business-logic-summary.md` §Docker validation. | Exec+run+parse-doesn't-break is now proven. **Still open below (#0d):** positive-*finding* extraction is unverified (benign targets return empty/clean); humble's parser is known to need a rewrite against its real section-based schema. | `.github/workflows/ci.yml` `probe-integration` |
| 0b | ~~Confirm tool version pins~~ **DONE 2026-07-14** (at first build): `httpx@v1.6.10`, `subzy@v1.1.0` (module path `LukaSikic/subzy`), testssl `v3.2.1`, `dirhunt==0.9.0`; humble & Corsy are **git clones** (no PyPI release), not pip. **Still: pin the `python:3.12-slim-bookworm` base and the git tools by digest/commit** (SECURITY-10/13). | Versions now verified to build & run; digest/commit pin remains for supply-chain integrity. | `Dockerfile`; Dependabot |
| ~~0c~~ | **subzy writable HOME under read-only rootfs** — **DONE 2026-07-14**: `ENV HOME=/scratch` in the image + `fsGroup: 10001` on the worker pod so the `/scratch` emptyDir is writable by the non-root uid. Validated with `docker run --read-only --tmpfs /scratch` (subzy PASS). | subzy caches its fingerprints DB in `$HOME`; read-only rootfs made `/home/app` unwritable. | `Dockerfile`, `deploy/k8s/worker.yaml` |
| ~~0d~~ | **Validate & fix positive-finding parser schemas** — **DONE (all 6) 2026-07-15**. r8 closed humble/httpx/Corsy (Juice Shop). **r9 closed testssl/dirhunt/subzy** against a committed `test/fixtures/vuln-target/` fixture + public badssl: real runs found **two fabricated parser schemas that emitted zero findings on real vulnerable output** and rewrote them — dirhunt (real signal `index_of_processors[].crawler_url.flags:"index_of"`, not `results[].flags:"directory-listing"`) and subzy (real signal string `status:"vulnerable"`, not boolean `vulnerable`); testssl schema confirmed correct. In-image end-to-end now asserts POSITIVE findings for all six (dirhunt CWE-548, subzy takeover, testssl CWE-1327, + humble/httpx/Corsy). | `internal/scan/checking/probe/{parsers,parsers_pbt_test,integration_test}.go`, `test/fixtures/vuln-target/`; summary `construction/scan/code/detection-fidelity-summary.md` |
| ~~0e~~ | **Provision subzy's fingerprints DB — ✅ DONE (r18, 2026-07-29).** subzy v1.1.0 has no fingerprint-path flag; it reads `$HOME/subzy/fingerprints.json` and self-fetches from a dead URL otherwise. `make subzy-db` provisions a pinned `fingerprints.json` (EdOverflow can-i-take-over-xyz @ `5bd4e12`) to a git-ignored path the operator mounts read-only at `/opt/subzy/fingerprints.json`; the adapter runs subzy with `HOME=/opt` so it reads it. Chosen operator-provisioning (not baked) — subzy's own community data, matching `make dev-env`/storageState. A precheck fails PS-025 to `tool-failed` on an absent/empty/malformed DB (never false-clean). Verified in-image (normal + read-only rootfs). | `make subzy-db`, `Dockerfile`, `deploy/{compose,k8s}` |
| 1 | **Run the Docker-backed integration suite** — `make test-integration` in a CI job with a Docker daemon | The Mongo/Valkey contract suites compile but were **never executed** (no Docker in the authoring env). Until they pass against real containers, the durable backends are unverified. | `internal/platform/{taskstore,workqueue}/*_test.go` (`//go:build integration`) |
| 2 | **Bump the Go toolchain to ≥ 1.26.5 and rebuild** | `govulncheck` flagged **GO-2026-5856** (crypto/tls ECH privacy leak) on our call paths (proxy/fetch/Mongo/Valkey TLS). Fixed in go1.26.5. No source change needed. | build pipeline / Dockerfile base |
| 3 | **Replace every image tag with a digest** (`@sha256:…`); no `latest` | SECURITY-10/13 supply-chain integrity | `Dockerfile`, `deploy/k8s/*.yaml`, `deploy/compose/docker-compose.yml` |
| 4 | **Provision secrets out-of-band** (Mongo URI w/ `tls=true&w=majority`, Valkey addr+password, API token, storageState) via sealed-secrets/vault | SECURITY-12 — never commit | `deploy/k8s/secret.example.yaml` is a template only |
| 5 | **Enforce TLS + auth on Mongo and Valkey**; enable Mongo replica set (3-node, majority) and Valkey AOF+noeviction | SECURITY-01/09, Q6=B/Q12=B durability | backend config |
| 6 | **Wire CI**: `go vet` · `gofmt` · `make arch-check` · `go test ./...` · `make vuln` · `make test-integration` (Docker job) · `make sbom` · image build w/ digest | RESILIENCY-04, SECURITY-10 | new pipeline |

## Recommended (non-blocking)
- Set the worker HPA to a **KEDA ScaledObject on Valkey stream length** (the manifest ships a portable CPU-based placeholder).
- Add resource requests/limits, PodDisruptionBudgets, and topology-spread constraints per AZ to the k8s manifests.
- Configure Mongo automated backups (oplog + daily snapshot, 7-day retention, encrypted) + a quarterly test-restore (RESILIENCY-12).
- Alerting on: auth failures, `/healthz` degraded, queue depth (consumer-group lag) growth, reaper reclaim rate, Mongo replication lag (RESILIENCY-05/07/14).

## Known limitations carried forward (honest — from code review + design)
1. ~~Split-topology mid-run recovery is not automatic.~~ **RESOLVED 2026-07-11 (ledger-lease increment).** Items carry a lease (owner + expiry, heartbeat-refreshed); a lease-safe periodic sweep (manager + all, `ReclaimExpired`) reclaims only expired-lease inflight items, so a dead worker's items are recovered mid-run without clobbering a live worker's. The dashboard scan list (`/api/introspect/scans`) is also live. Safe-reclaim invariant (at most one recorded outcome per item) proven by a stateful PBT. See `construction/platform/code/lease-business-logic-summary.md`.
2. ~~Probe adapters are stubs (`argv` nil).~~ **RESOLVED 6/7 2026-07-14 (increment r7).** Six probes wired (argv + JSON parser + bundled binary + process-group kill); changeme (PS-019) deferred (F5). Remaining: the in-image `probe-integration` job must run once on Docker to validate the hand-authored parser schemas against real output (#0a) and confirm the tool version pins (#0b). See `construction/scan/code/probe-business-logic-summary.md`.
3. **Not in this increment** (infra-plan #3–#6): ProgressReporter→OTel, Authorizer→OIDC/mTLS, ResponseCache→Valkey, FindingsStore→Mongo. The shared-instance placement for #5/#6 is pre-decided in `construction/shared-infrastructure.md`.
3b. **Observation transport correctness — ✅ RESOLVED (r16 + r17):** ~~(a) PS-009/018/023 dictionaries are
   shallow~~ **RESOLVED r16** — signatures are now OSS-distilled (gitleaks/ZAP/nuclei, `configs/signatures/`).
   ~~(b) `br`/`zstd`/`deflate` bodies undecoded + 512KB truncation silent~~ **RESOLVED r17** — bodies are
   decoded by Content-Encoding (output-bounded, bomb-safe) and truncation is surfaced in the report
   (`fetch/decode.go`, `Report.TruncatedURLs`). The former top detection-correctness gap is closed.
4. **Browser transport hardening** (infra-plan §2): Chromium provisioning, HTTPS CONNECT + MITM CA, session auto-refresh.

## Suggested next tasks (pick one)
- **Ledger lease** → safe split-topology recovery + dashboard scan list (closes the #1 limitation).
- **infra-plan #6 FindingsStore→Mongo** / **#5 ResponseCache→Valkey** (reuse the shared-instance placement already decided).
- **Probe binaries + real parsers** (infra-plan §3) — biggest coverage-fidelity gain.
