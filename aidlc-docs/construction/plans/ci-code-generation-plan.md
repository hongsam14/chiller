# Code Generation Plan — unit `ci`

**Created**: 2026-07-13
**Design**: `construction/ci/infrastructure-design/{infrastructure-design,deployment-architecture}.md` (approved)
**Requirements**: `requirements.md` §12 (r6)
**This plan is the single source of truth for Code Generation of unit `ci`.**

---

## Unit context

| | |
|---|---|
| **Unit** | `ci` — a single cross-cutting unit; not decomposable |
| **Stories** | None. This increment maps to **no user story** (User Stories was SKIPped: build-process improvement, no user-facing effect). Traceability runs to **CI-FR-01..07 / CI-NFR-01..06** and defects **X-1..X-8** instead. |
| **Dependencies on other units** | None at build time. The pipeline *exercises* all four units (domain/platform/scan/findings) but the units do not depend on it. |
| **Interfaces / contracts changed** | **Zero.** No seam, no interface, no BSON schema, no report format. PR-11 holds trivially. |
| **Database entities owned** | None. No migration. |
| **Runtime behaviour changed** | Three narrow places only: `ReadHeaderTimeout` on two HTTP servers, a backoff guard in `Retry.sleep`, and the Go stdlib underneath everything (toolchain 1.26.5). |

## Code locations (never `aidlc-docs/`)

```
go.mod                                   modify   toolchain
Dockerfile                               modify   digest-pinned bases
.golangci.yml                            modify   v1 -> v2 schema
Makefile                                 modify   pinned tools, new targets
.gitignore                               modify   deploy/compose/.env
.github/workflows/ci.yml                 create   the pipeline
.github/dependabot.yml                   create   digest refresh
deploy/compose/docker-compose.yml        modify   remove credential literals
deploy/compose/.env.example              create   dev placeholders
internal/platform/proxy/authproxy.go     modify   X-2 ReadHeaderTimeout
cmd/chiller/main.go                      modify   X-2 ReadHeaderTimeout
internal/findings/store/retry.go         modify   X-3 backoff guard
internal/findings/config/spec.go         modify   X-4 #nosec + X-5 errcheck
internal/findings/config/severity.go     modify   X-4 #nosec + X-5 errcheck
internal/findings/config/mapping.go      modify   X-4 #nosec + X-5 errcheck
internal/platform/api/introspect_test.go modify   X-5 errcheck
internal/scan/fetch/fetch_test.go        modify   X-5 errcheck
internal/domain/severity_test.go         modify   X-5 staticcheck
internal/findings/store/retry_test.go    modify   test for the new guard

aidlc-docs/construction/ci/code/business-logic-summary.md   create   (docs only)
```

**Brownfield discipline**: every file above except three is **modified in place**. No `*_new`, no `*_v2`, no duplicates.

---

## Ordering rationale (why this sequence, not another)

The toolchain goes **first** so that any breakage it causes is attributable to it and not confused with the source fixes. The lint config goes **before** the lint fixes, because a gate that does not load cannot tell you what to fix (that is X-1). The workflow goes **last**, because it codifies steps 1-10 as gates — writing it first would mean writing gates for a tree that cannot pass them.

---

## Steps

### Phase A — Toolchain (must be green before anything else moves)

- [x] **Step 1** — `go.mod`: `go 1.25.0` → `go 1.26.5`.
  *Verify*: `go build ./...`, `go vet ./...`, `go test ./...` all green **before proceeding**. (Already measured green on go1.26.5 during Requirements/Design — this step makes it declarative.)
  *Traces*: D-5, CI-NFR-02, X-6.

- [x] **Step 2** — `Dockerfile`: build base → `golang:1.26.5@sha256:079e59808d2d252516e27e3f3a9c003740dee7f75e55aa71528766d52bcfc16a`; runtime base → `gcr.io/distroless/static-debian12:nonroot@sha256:b7bb25d9f7c31d2bdd1982feb4dafcaf137703c7075dbe2febb41c24212b946f`. Replace the "pin by digest in production" placeholder comment with the fact that it *is* pinned.
  *Verify*: `docker build` succeeds.
  *Traces*: D-13, CI-NFR-01, X-7, SECURITY-10/13.

### Phase B — Make the lint gate real

- [x] **Step 3** — `.golangci.yml`: migrate v1 → **v2 schema** (`version: "2"`, `linters.default: standard`, `linters.settings.depguard`). Preserve the depguard rules **verbatim** — they mechanically enforce NFR-02 and are the one part of this config that was already correct.
  *Verify*: `golangci-lint run` **loads the config** (the current failure is `unsupported version of the configuration`) and reports the expected 8 findings.
  *Traces*: X-1, D-8.

- [x] **Step 4** — Fix the 8 lint findings: errcheck ×7 (`fh.Close`, `resp.Body.Close`, `w.Write` in `findings/config/*.go`, `platform/api/introspect_test.go`, `scan/fetch/fetch_test.go`), staticcheck ×1 (`domain/severity_test.go`, QF1001 De Morgan).
  *Verify*: `golangci-lint run` → **0 findings**.
  *Traces*: X-5, D-8.

### Phase C — Fix what gosec found (the gate is blocking, so these are not optional)

- [x] **Step 5** — **X-2, the real defect**: add `ReadHeaderTimeout: 10 * time.Second` to both `http.Server` literals — `internal/platform/proxy/authproxy.go:46` and `cmd/chiller/main.go:239`. **Only `ReadHeaderTimeout`** — no `WriteTimeout` on the proxy (it would truncate large relayed responses, which is precisely what PS-009 needs to read). Comment states the constraint, not the history.
  *Verify*: gosec G112 gone; `go test ./...` green (proxy tests exist).
  *Traces*: X-2, Q2=A, SECURITY-09.

- [x] **Step 6** — **X-3**: in `internal/findings/store/retry.go`, guard inside `sleep`: `if attempt < 1 { attempt = 1 }` before the shift. Move the safety from the caller into the function.
  *Verify*: gosec G115 gone.
  *Traces*: X-3, Q4=A.

- [x] **Step 7** — **X-3 test**: add a test in `internal/findings/store/retry_test.go` asserting `sleep(ctx, 0)` produces a **non-zero** backoff (i.e. the guard holds). Without this the guard is an unverified claim — and the whole increment exists because unverified claims shipped twice.
  *Verify*: new test passes; deliberately reverting the guard makes it fail.
  *Traces*: X-3, PBT-10 (example-based test complementing properties).

- [x] **Step 8** — **X-4**: `#nosec G304 -- operator-supplied config path (CLI flag), inside the trust boundary; not user input` on the three `os.Open` calls in `internal/findings/config/{spec,severity,mapping}.go`.
  *Verify*: gosec G304 gone; the reason is in the source.
  *Traces*: X-4, Q3=A.

### Phase D — Secrets (the gate is blocking; history must be clean)

- [x] **Step 9** — `deploy/compose/docker-compose.yml`: replace the two credential literals with required-env references — `CHILLER_EVIDENCE_KEY: ${CHILLER_EVIDENCE_KEY:?set it in deploy/compose/.env}` and the same for `CHILLER_TOKEN`. The `:?` form **fails loudly** instead of silently starting with an empty key. Keep the rotation comment (it explains a real constraint).
  *Traces*: SECURITY-12, Q1=A.

- [x] **Step 10** — Create `deploy/compose/.env.example` with the dev placeholders + comments; add `deploy/compose/.env` to `.gitignore`. `cp .env.example .env` restores the developer's one-command local stack.
  *Traces*: SECURITY-12, Q1=A.

- [x] **Step 11** — **Purge the value from history.** `git log -S` confirms the value was introduced in exactly one commit — **`a3ad705`, which is HEAD**. So no `git filter-repo` is needed (it is not even installed): fold Steps 9-10 into that commit with **`git commit --amend`**, so `a3ad705` never contained the secret.
  *Verify*: `gitleaks detect` over the **full history** → **0 findings**, with an **empty allowlist** (no `.gitleaksignore`).
  *Note*: this rewrites `a3ad705`'s SHA. Safe: nothing has been pushed. The stale `.claude/worktrees/probe-stub-honesty` worktree will point at the old SHA — it is untracked scratch and can be discarded.
  *Traces*: D-10, Q1=A, SECURITY-12.

### Phase E — The contract between CI and developers

- [x] **Step 12** — `Makefile`: pin every tool and make each gate runnable without pre-installation.
  - `test` → add `-race`
  - `test-integration` → `-timeout 30m` → **`-timeout 10m`** (measured runtime: 14.9 s; 10 m fires *before* the 25 m job cap so a hang yields a **goroutine dump**)
  - `lint` → `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.6.0 run`
  - `gosec` → **new**, `go run github.com/securego/gosec/v2/cmd/gosec@v2.22.9 -exclude-dir=.claude ./...` (the `.claude/worktrees/` scratch copy is untracked and would otherwise be double-scanned locally)
  - `vuln` → `@latest` → **`@v1.6.0`** (**X-8**: the supply-chain target was itself violating "no floating versions")
  - `secrets` → **new**, gitleaks container pinned by digest, full history
  - `sbom` → syft container pinned by digest (was a bare `syft` binary that is not installed)
  - `cover` → **new**, coverage percentage (report only)
  *Verify*: every target runs on a machine with only Go + Docker.
  *Traces*: CI-NFR-01, CI-NFR-04, X-8, §10 of the design.

### Phase F — The pipeline

- [x] **Step 13** — `.github/workflows/ci.yml`: triggers `pull_request` → `main` and `push` → `main`; `permissions: contents: read`; `concurrency` with `cancel-in-progress: true`; **three parallel jobs**:
  - **`fast`** — checkout(depth 1) · setup-go(`go-version-file: go.mod`, cache on PR / `cache: false` on main push) · `make build` · `gofmt -l` · `go vet` · `make arch-check` · `make lint` · `make test` (-race) · `make gosec` · `make vuln` *(continue-on-error)* · `make cover` *(continue-on-error)*
  - **`integration`** — checkout(depth 1) · setup-go · **`make test-integration`** · `timeout-minutes: 25`
  - **`supply-chain`** — checkout **depth 0** (a shallow clone would let `gitleaks` scan nothing and report green — the same class of failure as X-1) · `make secrets` · `docker build` (no push) · `trivy` HIGH/CRITICAL *(continue-on-error)* · `make sbom` → upload artifact
  Actions pinned by commit SHA: `actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd`, `actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16`.
  *Traces*: CI-FR-01..07, CI-NFR-01/03/04/05, Q6=A, Q7=A, Q8=A, Q10=A.

- [x] **Step 14** — `.github/dependabot.yml`: `docker` + `github-actions` ecosystems, weekly. Without this, digest pinning quietly decays into "abandoned" — and with no nightly (C1=D) nothing else would ever notice.
  *Traces*: Q9=A, CI-NFR-01.

### Phase G — Verify and document

- [x] **Step 15** — **Run every gate locally** with the exact pinned versions and command lines the workflow uses: `make build lint test gosec secrets vuln cover arch-check test-integration sbom` + `docker build` + trivy. Record actual results — not "should pass".
  *Traces*: §11 of the design.

- [x] **Step 16** — `aidlc-docs/construction/ci/code/business-logic-summary.md`: what was built, what was fixed (X-1..X-8), what was **measured**, and what remains unverifiable. Markdown only, docs directory only.

---

## Explicitly deferred to Build and Test (not this stage)

- **Push to `origin` and observe the real Actions run** (`gh run watch`). This is the first moment the YAML is executed by GitHub rather than read by me.
- **The adversarial test of the success criterion**: reintroduce the Valkey `Drain` bug on a scratch branch, confirm the pipeline turns **red**, revert. The entire justification for CI-FR-03 is a bug that two green Build&Test runs missed. Claiming the new pipeline would catch it, without showing it, would repeat exactly the failure this increment was built to end.

## Traceability

| Requirement / defect | Steps |
|---|---|
| CI-FR-01 (triggers) | 13 |
| CI-FR-02 (blocking gates) | 12, 13 |
| **CI-FR-03 (integration on every PR)** | **12, 13** — *the load-bearing requirement* |
| CI-FR-04 (image build + scan) | 13 |
| CI-FR-05 (vuln/trivy report-only) | 12, 13 |
| CI-FR-06 (coverage report) | 12, 13 |
| CI-FR-07 (SBOM) | 12, 13 |
| CI-NFR-01 (pinning) | 2, 12, 13, 14 |
| CI-NFR-02 (toolchain uniformity) | 1, 2, 13 |
| CI-NFR-03 (fast gates not serialized) | 13 |
| CI-NFR-04 (determinism) | 12, 13 |
| CI-NFR-05 (zero secrets) | 13 |
| CI-NFR-06 (pipeline integrity) | 13 + operator action (branch protection) |
| X-1 golangci config never loaded | 3, 4 |
| X-2 Slowloris (**real defect**) | 5 |
| X-3 backoff overflow | 6, 7 |
| X-4 G304 | 8 |
| X-5 errcheck/staticcheck | 4 |
| X-6 GO-2026-5856 | 1, 2 |
| X-7 mutable base tags | 2 |
| X-8 `govulncheck@latest` (**found during design**) | 12 |
| Secret in git history | 9, 10, 11 |

**Total: 16 steps.**
