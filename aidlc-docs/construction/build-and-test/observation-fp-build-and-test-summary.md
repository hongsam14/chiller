# Build and Test Summary — Observation 오탐 저감 (r16)

**Increment**: OSS-distilled observation signatures + FR-6 owning-signature-only well-known execution,
including the workflow code-review fix pass (7 findings). Single unit = `scan`
(`checking/observation`, `exec`) + `platform/orchestrator` fan-out/fold, `domain.WorkItem`,
`tools/sigimport` (dev-time generator, part of the main module). No new deployable; product
Dockerfile only gains `COPY configs` (already present); checker/platform interfaces unchanged.

## What changed (test-relevant)
- **New**: `internal/scan/checking/observation/signatures_config.go` (fail-closed `LoadRules` + Shannon
  entropy + `SecretRule`/`InternalRules`/`APIDocRules`); `tools/sigimport/main.go` (gitleaks/ZAP/nuclei
  → `configs/signatures/*.yaml`, provenance headers); `third_party/{gitleaks,zap,nuclei}/` pinned sources;
  `make signatures` / `signatures-check`; `.gitleaks.toml` allowlist.
- **Signatures rewired** to distilled rules: `sensitiveCommentSig` (gitleaks 221 secret rules, entropy +
  keyword pre-filter + allowlist + library-path skip, plus ZAP Private-IP internal disclosure with a Go
  boundary guard), `sessionInURLSig` (ZAP url params, token-boundary match, matrix path-params),
  `adminPageSig` (nuclei api-doc path + body-marker branch). `observation.Default()` → `New(Rules)`.
- **FR-6**: `WellKnownPaths() []WellKnownPath{Path, CWE}`, `domain.WorkItem.WellKnownCWE`, fan-out stamps
  it, `exec.observationForCWE` runs only the owning signature, `finalize.ranCheckers` mirrors it.
- **Review fixes folded in** (commit f35305e): api-docs generic path/marker drop; PS-018 token-boundary
  matching + bare user/pass drop; NonProdHosts multi-label FQDN guard; matrix path-param + phpsessid/sid;
  scan() single lower-casing; dead `ValuePatterns` removed. Config fixes made in the generator, not the
  generated yaml (config-is-data), regeneration idempotent.

## Local gates — ALL GREEN (2026-07-29)
| Gate | Command | Result |
|---|---|---|
| Build | `go build ./...` | OK |
| Vet | `go vet ./...` | OK |
| Vet (integration tag) | `go vet -tags integration ./...` | OK |
| Format | `gofmt -l internal/ cmd/ tools/ test/` | clean (empty) |
| Architecture | `make arch-check` | OK (data-plane ⊥ platform, kernel ⊥ units) |
| Unit + integration (race) | `go test -race ./...` | 23 pkg **ok**, 0 FAIL |
| SAST | `make gosec` | 0 findings (sigimport G301/G306 tightened, G304 nosec-justified) |
| Signature drift | `make signatures-check` | drift 0 (config == fresh regeneration) |
| Secret scan | `make secrets` (gitleaks, full history) | **no leaks found** |

## Test evidence added this increment
- **FP/FN corpus** `observation/distilled_sig_test.go` (blocking) — PS-009 secrets (real JWT fires in
  comment/JS; library asset & `password:!0` suppressed), PS-009 internal disclosure (RFC1918/EC2/staging
  FQDN fire; version-number & dev-mode/dev.js/qa-build FP silent), PS-018 (access_token/accessToken/
  PHPSESSID/sid/matrix-jsessionid fire; userType/passengers/boarding_pass/short-value silent), PS-013
  api-docs (distinctive path+marker fires; soft-404 & generic /docs page silent). Ground-truth rows for
  human-found #3 (accessToken), #10 (api-docs), #15 (JWT), #16 (staging host).
- **Loader** `observation/signatures_config_test.go` — `LoadRules` fail-closed (missing dir/file, bad
  regex, missing internal field), shipped-config non-empty, RFC1918 boundary-guard property test.
- **FR-6 structural** `scan/exec/exec_test.go` (`TestRun_WellKnownCWE_OwningSignatureOnly`) — a
  well-known item runs ONLY its owning signature, not the whole observation set.
- **Fold honesty mirror** `platform/orchestrator/orchestrator_test.go`
  (`TestRanCheckers_WellKnownCWE_MirrorsExec`) — the fold attributes a WellKnownCWE item to exactly the
  owning checker (owning-CWE / unknown-CWE / legacy paths), matching exec.
- **e2e** `test/platform_e2e_test.go` — 26-partition honest accounting still green with the real distilled
  config loaded (harness loads `configs/signatures`).

## Build & Test finding (fixed this stage)
- `gitleaks detect` scans committed history, so a `make secrets` run *before* committing new test
  fixtures cannot see them. The PS-018 fixtures (`access_token=abcdef123456`) tripped the gate once
  committed. Resolved by allowlisting the observation `*_test.go` corpus in `.gitleaks.toml` (commit
  1a4d5d8) — those files are the secret-scanner's own test inputs, secret-shaped by design.

## Invariants preserved
26-item coverage partition · one-signature-one-CWE ownership · checker/platform interfaces · product
Dockerfile · probe wiring · platform in-memory seams.

## Carried forward (honest — see aidlc-state Backlog → r16 도출 TODO)
- **P0 transport**: `br`/`zstd`/`deflate` bodies undecoded (global FN) + 512 KiB truncation silent.
- `changeme` (PS-019) still stubbed; `subzy` needs its fingerprint DB provisioned (#0e).
- Review #5 (intended trade): PS-009 generic `password:`/`api_key`/`todo` anchors removed — low-entropy
  hardcoded-credential / dev-note tail is an accepted coverage loss (their re-addition was a major FP
  source).
