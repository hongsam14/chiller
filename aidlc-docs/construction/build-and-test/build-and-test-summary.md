# Build and Test Summary — chiller

**Date**: 2026-07-11 (r2 — **infrastructure increment** folded in) · Prior: 2026-07-10 (MVP, 4 units).
**Scope of r2**: platform U1 seam-swap (`TaskStore`→Mongo, `WorkQueue`→Valkey/Streams) + introspection UI + deploy assets + post-review hardening.

## Build Status
- **Build Tool**: Go (verified go1.26.4), module `github.com/enki-offen/chiller`.
- **Build Status**: ✅ **Success** — `go build ./...` clean · `go vet ./...` clean · `gofmt -l` clean.
- **Build Artifacts**: `chiller` — single binary, 4 roles (`all|manager|worker|proxy`); container image via `Dockerfile` (distroless nonroot).
- **Architecture guard**: ✅ `make arch-check` OK (data plane never imports platform; kernel imports no unit — the bson tags added no imports).
- **Direct dependencies**: `gopkg.in/yaml.v3` · `playwright-go v0.5200.1` · `retryablehttp-go v1.3.17` · **`go.mongodb.org/mongo-driver v1.17.9`** · **`github.com/redis/go-redis/v9 v9.20.1`** · **`testcontainers-go v0.37.0`** (test-only, + mongodb/redis modules). All version-pinned; `go.sum` committed (SECURITY-10).

## Test Execution Summary

### Unit Tests (default `go test ./...`, no external services)
- **Total**: **108 top-level test functions** (130 including subtests), 20 packages.
- **Passed**: all · **Failed**: **0**.
- **Race detector**: ✅ `go test -race ./...` clean.
- **Coverage**: module total **54.3%** of statements (MVP r1 was 66.0%). The drop is **expected and is not a regression in tested behavior**: the increment added a large block of code that only executes under the `integration` tag (the Mongo/Valkey backends), so its statements are in the denominator while its tests do not run in the default profile. mem-path coverage is unchanged.
- **New in r2**: seam **contract suites** (`taskstore/contract_test.go`, `workqueue/contract_test.go`) parameterized over constructors — the identical suite the durable backends must pass (PR-11a); **PBT** (serialization round-trip, ledger command-sequence vs an in-test model oracle, queue multiset/FIFO; seeds logged); **UI/health** tests (security headers, authz fail-closed, deep `/healthz` → 503); **ingest default-ceiling** regression test.
- **Status**: ✅ **Pass**.

### Integration Tests — Tier 1: in-process (`test/` package, no external services)
- **Scenarios (7)**: coverage-wiring 26-partition; e2e submit→report + finding; fault isolation; worker-pool concurrency; financial auth-proxy session injection; resume idempotent; stop transitions.
- **Passed**: all · **Failed**: 0. **Status**: ✅ **Pass**.

### Integration Tests — Tier 2: durable backends (`-tags integration`, requires Docker) — **NEW in r2**
- **What**: `make test-integration` starts **real Mongo and Valkey containers** (testcontainers-go) and runs the **same contract suites the mem impls pass** (PR-11a): CAS single-winner, ledger/unfinished semantics, resume-no-reclaim, `ListScansByState`, FIFO / block / ctx-cancel / Drain, `Stats`.
- **Status**: ⚠️ **Compiles clean (`go vet -tags integration` OK) but WAS NOT EXECUTED** — no Docker daemon was available to the test run in this environment. **It must be run in a Docker-capable CI job before the durable backends are trusted in production.** This is r2's one honest verification gap.

### Performance Tests
- **Numeric SLOs**: throughput now scales with the **worker-pool replica count** (Q2=B; HPA on queue depth). The p95 scan-duration SLO is still to be measured against the chosen fleet (Operations).
- **Properties verified**: worker-pool parallelism; per-host rate ceiling; financial single-session T-bound.
- **Status**: ✅ **Pass (qualitative)**; scaling-curve method documented in `performance-test-instructions.md`.

### Security Tests
- **Dependency vulnerability scan** (`make vuln` / govulncheck) — **NEW in r2, actually executed**:
  - ⚠️ **1 vulnerability affects our code**: **GO-2026-5856** — Encrypted Client Hello privacy leak in the Go standard library `crypto/tls`. **Present in `crypto/tls@go1.26.4`; fixed in `go1.26.5`.** Reached from `proxy.Serve`, `fetch.Fetch`, `taskstore.Mongo` (tls.Dial) and `workqueue.NewValkey` (tls.DialWithDialer).
  - **Remediation**: **bump the Go toolchain to ≥ 1.26.5 and rebuild.** No source change needed. No third-party dependency we actually call is affected (15 further vulns exist in required modules but sit on no call path).
- **Fail-closed authz**: ✅ unit-proven (`/scans` and `/api/introspect/queue` reject a missing/wrong Bearer).
- **HTTP security headers** (SECURITY-04 — newly applicable, because the UI now serves HTML): ✅ unit-proven on `/ui` (CSP, HSTS, `X-Content-Type-Options`, `X-Frame-Options: DENY`, `Referrer-Policy`).
- **Retained from r1**: outbound scope containment (incl. redirect + host-probe path), per-host rate budget, evidence bounding, financial single-session invariant, ingest validation.
- **Secrets**: connection strings/tokens injected via env/Secret; never in source or images (SECURITY-12).
- **Status**: ⚠️ **Pass with one actionable finding** (Go toolchain bump).

### Contract Tests
- ✅ The seam contract suites ARE the Infra-swap surface: one suite, run against mem by default and against Mongo/Valkey under the `integration` tag.

### Code Review (workflow-backed, high effort) — **NEW in r2**
- 22 agents; **10 verified findings, all fixed** and re-verified green. Headline fixes: crash-recovery wiring (boot-time `recoverOrphans` + `ListScansByState`; deliberately boot-only / `all`-role-only to avoid a double-execution hazard), manager no longer consumes items it cannot dispatch, Mongo↔mem seam not-found parity, unbounded stream growth (MAXLEN backstop), queue-depth metric corrected (consumer-group lag, not XLEN), Stats error propagation, and a **pre-existing SR-1 bug**: the MaxURLs ceiling was silently bypassed whenever the option was omitted.
- **Status**: ✅ **Pass**.

## Overall Status
- **Build**: ✅ Success
- **All default tests**: ✅ Pass (108/108, 0 failures, race-clean)
- **Durable-backend contract suite**: ⚠️ compiles, **not executed** (needs Docker/CI)
- **Security scan**: ⚠️ 1 actionable stdlib finding (requires Go ≥ 1.26.5)
- **Ready for Operations**: **Yes, conditionally** — run `make test-integration` in a Docker-capable CI job and bump the Go toolchain before trusting the durable backends in production.

## Known limitations carried forward (honest)
1. **Split-topology mid-run recovery**: a worker that dies while the manager lives is not auto-recovered. Safe recovery there needs lease/heartbeat timestamps on the ledger — blindly resetting `inflight` would double-execute an item a live worker is still processing. The default single-process (`all`) topology **is** fully covered by boot recovery. Tracked follow-up.
2. **Probe adapters remain stubs** (`argv` nil) — real CLI binaries + parsers are infra-plan §3.
3. **infra-plan #3–#6 not in this increment**: OTel exporter, OIDC/mTLS Authorizer, ResponseCache→Valkey, FindingsStore→Mongo.
4. **Browser transport**: Chromium provisioning, HTTPS CONNECT + MITM CA hardening, session auto-refresh (infra-plan §2).

## Next Steps
Proceed to Operations. Before production: (a) execute the Docker-backed integration suite, (b) bump Go to ≥ 1.26.5, (c) replace image tags with digests.
