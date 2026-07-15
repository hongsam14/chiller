# Code Summary — Unit `scan` (U2)

**Stage**: 🟢 CONSTRUCTION → `scan` → Code Generation · **Date**: 2026-07-08
**Verified**: `go build/vet/test ./...` all pass, `gofmt` clean, `make arch-check` OK. Includes a **cross-unit integration test** (`test/coverage_wiring_test.go`) proving the real config wiring covers all 26 KISA items.

---

## Files (application code — workspace root)

### `internal/scan/`
| Package | Contents | Story · Rule |
|---|---|---|
| `ingest` | `URLIngestor.Ingest` — validate format/scheme/host/bounds, fail-closed | US-003 · SR-1 |
| `safety` | `ApprovalGate` (request ∩ global allowlist, fail-closed) · `RateLimiter` (per-host token bucket) | US-004/005 · SR-3/4 |
| `fetch` | `ResponseCache`+`InMemory`; `Fetcher.Fetch` (gate→limiter→GET once→cache; reuse); **`Discoverer` seam** + `StaticDiscoverer` (`<script src>`, approved-host, no exec) | US-002 · SR-2/5 |
| `checking/observation` | `Signature` (one CWE each) + `AsChecker` adapter (id `observation:CWE-N`, URL-scoped, per-sig isolation) + **10 signatures** (tight anchors) | US-006/008-013/030-033 · SR-6 |
| `checking/probe` | `Adapter` framework (`exec.CommandContext` timeout + Parser hook + `OwnedCWEs`/`PinnedVersion`) + **7 adapters** (parsers stubbed; binaries at Infra) | US-007 · SR-7 |
| `checking/registry` | `CheckerRegistry.Register/List/OwnershipForFindings` (→ `CWEOwnership`) | NFR-02 |
| `exec` | `Executor.Run` — Fetch+Cache phase ①②③④ then run scope-matched checkers, aggregate `WorkItemOutcome{Findings,Failed,NotApplicable}` | US-027 · SR-8/9 |

### Config
`configs/mappings/kisa.yaml` rewritten to a coherent 22-item map — distinct CWE per checker (no ownership collisions), each CWE→its PS item; `attackTechnique` PS-025→subzy/T1584.001.

### Tests (per package + integration)
- ingest reject cases; safety gate intersection + token-bucket burst/throttle (frozen clock); fetch cache-reuse (1 GET) + fail-closed + discoverer approved-only; **observation anchor property** (positive→1 / negative→0 or N/A across all 10); probe framework exec+parse + stub-clean; registry ownership (17 checkers) + idempotent; executor aggregation (findings/failed/N-A) + fetch-failure-all-tool-failed + host-scoped-no-fetch.
- **`test/coverage_wiring_test.go`** — loads real configs, builds registry from observation+probe, runs `findings.coverage.New` → boot completeness passes; a clean scan yields a valid 26-partition (PS-025 Checked via attacker axis, PS-014→ASM, PS-005→Nuclei).

## Decisions realized
- **질문1**: one signature = one CWE; each a `domain.Checker` (`observation:CWE-N`); engine `OwnedCWEs()` → registry → `CWEOwnership`.
- **질문2**: `Check` returns `domain.CheckResult{Findings, NotApplicable}`; executor aggregates N/A into `WorkItemOutcome.NotApplicable`.
- **질문3/4/5**: subprocess-exec framework (timeout, version-pinned); approval = request ∩ allowlist fail-closed; per-host token bucket.
- **Discovery timing**: `Discoverer` is step ② of the Fetch+Cache phase (after main fetch, before JS fetch); `StaticDiscoverer` (MVP) built, `PlaywrightDiscoverer` (r5 A-6, Post-MVP) drops into the same seam.

## Reuse / boundaries
Imports `domain` only (arch-check green). Emits `domain.Finding`/`WorkItemOutcome`; exposes `CWEOwnership` for findings. Does not schedule (platform) or account/report (findings).

## Code review fixes (workflow-backed, high — all applied)
5 High + 6 Medium + 2 Low, each with a regression test:
- **H1** redirect gate bypass → `client.CheckRedirect` re-authorizes + charges rate per hop (SSRF/scope-escape closed).
- **H2** host-scoped probes un-gated → `exec.New(...)` takes an `ApprovalGate`; host path fails closed before I/O.
- **H3** rate=0 disabled throttling by default → `ingest` defaults RatePerHost=5/Burst=rate/MaxURLs=500; `URLSet.Options` carries the effective config.
- **H4/M4** unbounded timeout + no cancellation → `Fetch(ctx,…)`/`Run(ctx,…)` with explicit per-request `context.WithTimeout` regardless of client.
- **H5** unbounded JS fan-out → discovery capped at 100/page.
- **M1** burst<1 infinite spin → `NewRateLimiter` clamps burst≥1. **M2** comment-sig FP (visible-body "Password:") → scans only comments+JS. **M3** git-sig FP → requires `.git` path. **M5** per-call regex → hoisted. **M6** `pathOf` query → `net/url`.
- **L1/L2** (findings) → `boundUnion` no throwaway alloc; snippet bound unified as `domain.MaxEvidenceSnippets`.

**⚠ Public API changed (platform wiring must adopt):** `Fetcher.Fetch(ctx, scanID, t)` · `Executor.Run(ctx, wi, cache)` · `exec.New(checkers, f, d, gate)` · `ingest` returns `URLSet.Options` (build the limiter from it, not raw request options).

## Deferred (documented)
- Real per-tool output parsers (7 probe adapters currently run-clean stubs; pinned per C-5).
- CLI binary bundling + version pinning → Infrastructure/Build (SEC-10).
- Valkey `ResponseCache` impl → Infrastructure (in-memory ships now).
- **Post-MVP `PlaywrightDiscoverer`** (Chromium network capture, r5 A-6) → same `Discoverer` seam; MVP dynamic-JS gap is the documented §2.2 Known limitation.
- Authoritative CWE↔PS-ID rows → C-5 (current mapping is coherent + indicative).

---

## Post-MVP improvement — retryablehttp Fetcher resilience (2026-07-10)

Adopted `github.com/projectdiscovery/retryablehttp-go v1.3.17` in `internal/scan/fetch` for transient-error resilience (fewer false `StatusToolFailed`). Ecosystem-aligned with the PD tools chiller already uses (httpx/subzy/nuclei).

**What changed** (`fetch.go`): the gated `http.Client` is now wrapped by a retryablehttp client (`NewWithHTTPClient`) with bounded exponential backoff. Default `WithRetries(2)` (option; 0 disables). `Fetch` issues via `f.rc.Do` instead of `client.Do`.

**Safety invariants preserved (the whole point):**
- **Per-host rate budget (SR-4)**: the naive risk was N retries spending 1 token. `checkRetry` calls `lim.Acquire(host)` on every retry decision, so **each attempt (initial + retries) spends exactly one token** — proven by `TestFetcher_RetriesTransientThenSucceeds_ChargesBudgetPerAttempt` (3 attempts → 3 tokens). Host is threaded via a ctx value so it's known even when a transport error yields a nil response.
- **Redirect re-gate (SR-3)**: chiller's `CheckRedirect` stays on the wrapped client. retryablehttp keeps a second, ungated HTTP/2 fallback client (`HTTPClient2`) used only on a malformed-HTTP/2 error — **pointed at the SAME gated client** (`f.rc.HTTPClient2 = client`) so no fail-open seam exists on any path.
- **Scope rejection is not transient**: `checkRetry` short-circuits `errors.Is(err, safety.ErrNotApproved)` → no retry, fail fast.
- **5xx is not retried**: retryablehttp's `CheckRecoverableErrors` retries transport errors only; a 5xx is a valid observed response (a passive scanner must not hammer). Proven by `TestFetcher_5xx_NotRetried`.
- **Timeout (SR-8)**: retryablehttp `Options.Timeout=0`; the per-request ctx deadline remains the single bound covering all attempts. Body still bounded by chiller's own `io.LimitReader`.

**Tests added** (`retry_test.go`): retry-then-succeed + per-attempt budget, `WithRetries(0)` disables, 5xx not retried. Full module green (`build/vet/test/-race`, `gofmt -l` clean, `make arch-check` OK). New direct dep pulls the PD/zmap tree (fastdialer, zcrypto) — acceptable and already transitively present via the ecosystem.
