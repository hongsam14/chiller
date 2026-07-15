# Code Generation Summary — unit `ci`

**Date**: 2026-07-13
**Plan**: `construction/plans/ci-code-generation-plan.md` (16 steps, all [x])
**Design**: `construction/ci/infrastructure-design/`

---

## What was built

A GitHub Actions pipeline (`.github/workflows/ci.yml`) with **three parallel jobs**, plus the repository changes needed to make its gates pass.

| Job | Gates | Blocking |
|---|---|---|
| `fast` | build · gofmt · vet · arch-check · golangci-lint · gosec · unit tests (`-race`) | yes |
| | govulncheck · coverage | report only |
| **`integration`** | **`go test -tags integration` against real Mongo + Valkey (testcontainers)** | **yes** |
| `supply-chain` | gitleaks (full history) · `docker build` | yes |
| | trivy image scan · syft SBOM | report only |

**Why this exists**: the durable `WorkQueue` shipped broken in two consecutive increments while Build&Test was green both times. The Valkey `Drain` bug made `StopScan` not stop a scan, and nothing caught it because nothing ran the durable backends. The load-bearing gate is therefore `integration` (CI-FR-03); every other gate supports it.

---

## Defects fixed (all found by actually running the tools, not by reading code)

| # | Defect | Fix | Verified |
|---|---|---|---|
| **X-1** | **`.golangci.yml` was v1 schema — golangci-lint v2 refused to load it. The lint config had never run**, despite being committed and referenced by `make lint`. The gate was decorative. | Migrated to v2 schema | `golangci-lint run` → **0 issues** (it now loads) |
| **X-2** | **`ReadHeaderTimeout` missing on both HTTP servers → Slowloris exposure** (gosec G112). A real availability weakness in shipped code. | `ReadHeaderTimeout: 10s` on `authproxy.go` + `main.go`. **No `WriteTimeout`** — the proxy relays arbitrary upstream responses (large JS bundles that PS-009 must read); a write deadline would truncate them intermittently under load. | gosec G112 gone; proxy tests pass |
| **X-3** | `Retry.sleep` backoff could collapse to **zero** — `base << uint(2*(attempt-1))` with `attempt < 1` wraps the shift count. Latent (callers pass ≥1), but the safety lived in the caller. | Guard moved **into** the function, and the shift replaced with repeated multiplication so there is no int→uint conversion left to get wrong. | New test `TestSleepNeverCollapsesToZeroBackoff`. **Mutation-checked**: removing the guard makes it fail (backoff drops to **17 µs** instead of 50 ms). |
| **X-4** | gosec G304 ×3 (config loaders open a variable path) | `#nosec G304` with a written reason: operator-supplied CLI path, inside the trust boundary. Constraining it to a fixed directory would break the legitimate case and buy no defence. | gosec → 0 issues, 3 nosec |
| **X-5** | errcheck ×7 + staticcheck ×1 | Fixed | lint → 0 |
| **X-6** | `go.mod` was `go 1.25.0`, Dockerfile `golang:1.25` → **GO-2026-5856 (crypto/tls) live in every artifact we produced** | Toolchain pinned to **Go 1.26.5** in `go.mod`, Dockerfile, and CI (`go-version-file: go.mod`, so they cannot drift) | `govulncheck` → **0 vulnerabilities affecting the code** |
| **X-7** | Base images on **mutable tags** | Digest-pinned: `golang:1.26.5@sha256:079e5980…`, `distroless/static-debian12:nonroot@sha256:b7bb25d9…` | `docker build` OK |
| **X-8** | **`Makefile`'s `vuln:` target used `govulncheck@latest`** — a floating version, violating SECURITY-10 ("pinned tool versions, no `latest`") **inside the very target meant to enforce supply-chain hygiene** | Pinned to v1.6.0 | — |
| **Secret in git history** | `CHILLER_EVIDENCE_KEY` (a dev placeholder, but a true positive in *form*) committed in `a3ad705`, plus a hardcoded `CHILLER_TOKEN` literal. `gitleaks detect` scans **history**, so deleting the line would not have helped. | Both moved to `deploy/compose/.env` (gitignored) with `${VAR:?}` required-env refs; `.env.example` ships **no key material at all**; `make dev-env` generates a random key. The commit that introduced it (`a3ad705`, which *was* HEAD) was **amended**, so it never contained the secret. | `gitleaks detect` over the **full 11-commit history** → **"no leaks found"**, with an **empty allowlist** (no `.gitleaksignore`) |

### Corrections to earlier claims in this increment

- **"8 lint findings" was wrong — the real number was 11.** The first measurement ran golangci-lint with the config file outside the repo tree (in a scratch directory), which changed its analysis root and silently under-reported by 3 (`authproxy.go:86`, `proxy_test.go:80`, `fetch.go:196`). Measuring with the config in its real location surfaced them. All 11 are fixed.
- **"The integration suite takes minutes to tens of minutes" was wrong — it takes 14.9 seconds.** That measurement is what corrected the timeout design (§ below).
- **gosec caught a compile error I introduced.** After adding `ReadHeaderTimeout` I had not imported `time` in `authproxy.go`, and the sub-package test I ran did not cover it. The gate found it before I did — which is the entire argument for this increment, demonstrated on its own author.

---

## Design decisions worth restating

**Zero secrets.** The pipeline requires no repository secrets at all: it builds an image but never pushes one, and deploys nothing. This is not a side-effect of a small scope — it is the strongest guarantee available. A compromised workflow, a malicious dependency, or a poisoned action **has nothing to steal and nowhere to deploy**. Any future increment that adds a registry push or a signing key trades some of this away and should say so.

**Zero third-party Actions.** Only `actions/checkout`, `actions/setup-go`, `actions/upload-artifact` — all first-party, all pinned by commit SHA. Every other tool runs as a Go module at an exact version or a container pinned by digest. Seven third-party actions' worth of supply-chain surface, removed.

**`fetch-depth: 0` on the supply-chain job is load-bearing.** gitleaks scans history. Under the default shallow clone it would examine almost nothing and report green — a gate that inspects nothing but passes. That is the same class of failure as X-1, and it would have been invisible.

**Timeouts, corrected by measurement.** The integration suite runs in ~15 s. The choice was a 25-minute job cap under a 30-minute Go timeout — but that ordering means a hang kills the *job* first, so Go never prints its goroutine dump and we learn *that* it hung, never *where*. (The last hang cost 25 minutes and had to be diagnosed by reading code.) Inverted: **Go `-timeout 10m` fires first with a full stack dump; the 25-minute job cap is the backstop.**

**Cache on PRs, no cache on `main`.** Fast feedback where a stale cache only costs the author time; a clean build for what actually lands on the default branch, so a poisoned cache can never become the artifact of record.

**Makefile is the contract.** CI invokes `make` targets, not raw commands, and every tool runs without pre-installation (`go run pkg@version`, digest-pinned containers). X-1 happened precisely because a config existed for a tool nobody had installed. `make ci` runs every blocking gate in one command.

---

## Verification — what was actually run (not "should pass")

| Gate | Result |
|---|---|
| `make build` | OK |
| `gofmt -l` | clean |
| `go vet ./...` | OK |
| `make arch-check` | OK — **NFR-02 upheld**: data plane does not import platform; domain imports no unit |
| `make lint` (golangci-lint v2.6.0) | **0 issues** — including depguard, which independently confirms NFR-02 |
| `make gosec` (v2.22.9) | **0 issues**, 3 nosec (documented) — exit code 0 |
| `make test` (`-race`) | **FAIL 0** |
| **`make test-integration`** (real Mongo + Valkey) | **FAIL 0**, 14.9 s |
| `make secrets` (gitleaks, full history) | **no leaks found** |
| `docker build` | OK (46.6 MB) |
| **`make ci`** (every blocking gate) | **exit 0 — all blocking gates passed** |
| `make vuln` (govulncheck v1.6.0) | **0 vulnerabilities affecting the code** |
| `make cover` | 50.0% (unit only; report-only by decision) |
| `make sbom` (syft) | 161 KB CycloneDX |
| `trivy` image scan | see below |
| `.github/workflows/ci.yml` | **valid YAML; `actionlint` v1.7.7 → 0 findings** |

### Trivy: what it found, and what was done about it

**First run** — OS packages (distroless debian12): **0**. Go modules linked into the binary: **10 HIGH** — `golang.org/x/crypto v0.51.0` (9 CVEs) and `go-jose/v3 v3.0.4` (1 CVE), both transitive (mongo-driver → pkcs8 → x/crypto; playwright-go → go-jose).

**Trivy and govulncheck disagreed, and both were right.** govulncheck does *reachability* analysis — our code never calls the vulnerable paths, so it reported 0. Trivy scans at *package* level — the vulnerable code is linked into the binary, so it reported 10. Non-blocking by decision (C1=D, D-12), **but a gate being non-blocking was never a decision to leave the findings there.** Raised at the approval gate; the user chose to fix.

**Fixed** (`go get golang.org/x/crypto@v0.52.0`, `go get github.com/go-jose/go-jose/v3@v3.0.5`, `go mod tidy`):

| Target | HIGH/CRITICAL, after |
|---|---|
| OS packages (distroless debian12) | **0** |
| Go modules linked into the binary | **0** |
| govulncheck | **0 affecting the code** |

All blocking gates re-run after the bump: **`make ci` → exit 0**, FAIL 0, `docker build` OK.

---

## Not done here (deferred to Build and Test, deliberately)

1. **Push and observe the real Actions run.** Everything above was executed locally. The workflow YAML has been parsed and linted, but **GitHub has not yet run it** — until it does, "the pipeline is green" remains a claim, not a fact.
2. **The adversarial test**: reintroduce the Valkey `Drain` bug on a scratch branch, confirm the pipeline turns **red**, revert. The whole justification for CI-FR-03 is a bug two green runs missed. Asserting the new pipeline would catch it, without showing it, would be the same error one level up.

## Known limitations (carried, not hidden)

- **No nightly schedule** (C1=D) → a CVE published during a quiet period is not detected until someone opens a PR.
- **`govulncheck` and `trivy` are non-blocking** → a vulnerable dependency can merge; the report has to actually be read.
- **Branch protection is an operator action**, not a file. Until someone requires these checks on `main`, the gates are advisory and can be bypassed by pushing directly.
- **The history rewrite changed `a3ad705` → `4762ab5`.** Nothing was pushed, so the blast radius is this machine; the stale `.claude/worktrees/probe-stub-honesty` worktree points at the old SHA and is untracked scratch.
