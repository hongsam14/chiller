# Security Test Instructions — chiller

The `security-baseline` and `resiliency-baseline` extensions are **enabled (blocking)**. chiller is itself a security tool that sends outbound traffic, so its own safety invariants are load-bearing and must be tested, not just documented.

## 1. Dependency & supply-chain checks
```bash
go vet ./...                          # static checks (must be clean)
govulncheck ./...                     # known-vuln scan (install: go install golang.org/x/vuln/cmd/govulncheck@latest)
go mod verify                         # module checksums match go.sum
```
- **Expected**: vet clean; `govulncheck` reports no reachable vulnerabilities. Pin playwright-go to **v0.5200.1** (audited); reject v0.6100.0 (module-path mismatch).

## 2. Fail-closed authorization (ScanAPI, PR-7)
- **Invariant**: no scan is created/started without a valid Bearer token; an **empty configured token denies all** (misconfiguration ≠ open door).
- **Tests** (`internal/platform/api`): `TestPostScan_Unauthorized_FailClosed` (no/wrong token → 401, orchestrator never invoked), constant-time token compare (`crypto/subtle`).
```bash
go test -run 'Unauthorized|Authoriz' ./internal/platform/api/
```

## 3. Outbound scope containment (the core scanner-safety property, SR-3)
- **Invariant**: nothing goes outbound to an unapproved host — including via **redirect** (each hop is re-authorized) and via the **host-scoped probe path** (gated before the probe runs). Fail-closed.
- **Tests** (`internal/scan/safety`, `internal/scan/fetch`, `internal/scan/exec`):
  `TestFetcher_RedirectToUnapprovedHost_Blocked`, host-gate fail-closed in exec, approval ∩ allowlist intersection.
```bash
go test -run 'Approv|Redirect|Unapproved|FailClosed' ./internal/scan/...
```

## 4. Per-host rate budget (abuse prevention, SR-4)
- **Invariant**: no host is hit faster than its token-bucket budget; burst is clamped ≥1 so limiting can never spin.
- **Test**: `TestRateLimiter_*` (`internal/scan/safety`).
- **Retry budget (retryablehttp, DONE 2026-07-10)**: each retry attempt charges a per-host token — no N-retries/1-token bypass. Verified by `TestFetcher_RetriesTransientThenSucceeds_ChargesBudgetPerAttempt` (`internal/scan/fetch/retry_test.go`); the HTTP/2 fallback client is pointed at the same gated client so the redirect re-gate holds on every path.

## 5. Evidence & data-at-rest hygiene (FR-12/SEC-01/03)
- **Invariant**: response bodies are bounded (`io.LimitReader`, 512KB) and never persisted as findings; evidence snippets are bounded (`domain.MaxEvidenceSnippets`) and redacted before persistence.
- **Tests**: normalize/dedup evidence bounding (`findings/normalize`, `findings/dedup`); fetch body cap.

## 6. Financial auth-proxy integrity (PR-12)
- **Invariant**: a single authenticated `BrowserSession`; workers cannot fan out into multiple sessions (single-session invariant). The login keep-alive tab is excluded from the work pool T (no concurrency starvation, no extra sessions).
- **Tests** (`internal/platform/proxy`): session injection, work-cap == T with keep-alive reserved, ctx-cancel shutdown.
- **Deferred hardening (Infra)**: HTTPS CONNECT + bundled MITM CA issuance/trust, TLS interception hardening, storageState secret handling, session auto-refresh. Track as Infra security tasks.

## 7. Input validation (ingest, SR-1)
- **Invariant**: malformed/over-limit URL sets are rejected fail-closed (no partial scan); MaxURLs bounds fan-out; per-page JS discovery is capped (amplification guard).
```bash
go test -run 'Ingest|Discover|Cap' ./internal/scan/...
```

## Run the security-relevant subset
```bash
go test -race ./internal/scan/safety/... ./internal/scan/fetch/... ./internal/scan/exec/... \
              ./internal/platform/api/... ./internal/platform/proxy/...
```
**Expected**: all pass. These encode the blocking security-baseline requirements as executable checks.

---

# Dependency Vulnerability Scan (added 2026-07-11 — SECURITY-10)

## Run
```bash
make vuln
# = go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```
Wire this into CI **before** the image build. Exit status 3 = vulnerabilities affecting called code.

## Result of the 2026-07-11 run — ⚠️ 1 finding affecting our code
| ID | Package | Found in | Fixed in | Reached from |
|---|---|---|---|---|
| **GO-2026-5856** | Go stdlib `crypto/tls` (Encrypted Client Hello privacy leak) | `crypto/tls@go1.26.4` | **`go1.26.5`** | `proxy.Serve`, `fetch.Fetch`, `taskstore.Mongo` (tls.Dial), `workqueue.NewValkey` (tls.DialWithDialer) |

**Remediation**: bump the Go toolchain to **≥ 1.26.5** and rebuild. **No source change required.**
No third-party dependency on any call path is affected (15 further advisories exist in required modules but are unreachable from our code).

## Also verified (unit-proven)
- **Fail-closed authz** on `/scans` and `/api/introspect/queue` (missing/wrong Bearer → 401).
- **HTTP security headers on `/ui`** (SECURITY-04, newly applicable now that HTML is served): CSP, HSTS, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy`.
- **Secrets** are injected via env/K8s Secret — never in source, configs, or the image (SECURITY-12).

## SBOM
```bash
make sbom   # syft → sbom.cyclonedx.json (required for production images)
```
