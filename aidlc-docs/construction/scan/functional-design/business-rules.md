# Scan — Business Rules

**Stage**: 🟢 CONSTRUCTION → `scan` (U2) → Functional Design (artifact 2/3) · **Date**: 2026-07-08
**Basis**: 질문1-5=A · domain R1-R10 · requirements r5.

---

## SR-1 — Ingestion / validation (US-003, SEC-05/15)
- Reject before any outbound: malformed URL, host not in `Approved`, list size > `MaxURLs`, options out of range → error (fail-closed, SR-3).
- No crawling/discovery except the C-8 `<script src>` shallow step (SR-5). Response bodies not supplied upstream → chiller re-fetches (C-1).

## SR-2 — Single-fetch reuse (US-002, FR-02)
- Each in-scope URL's HTML is fetched **once** and cached (HAR). URL-scoped checkers (Observation, humble, httpx) read the cache — out-going HTML GET per URL = 1 (incl. resume, SR-8).
- Host-scoped tools (testssl/dirhunt/subzy) bypass the cache (EC3), own protocol I/O.

## SR-3 — Approval gate (US-004, FR-10, SEC-11/14/15)
- Effective approval = per-scan `Approved` ∩ global allowlist (if configured; else `Approved`).
- Every probe invocation and every Observation/discovery GET calls `Authorize` first; unapproved host ⇒ reject, 0 out-going requests (fail-closed).
- Append-only audit of who scanned what (SEC-14).

## SR-4 — Rate limiting (US-005, NFR-13)
- Per-host token bucket (`RatePerHost`/`Burst`). Over-limit ⇒ delay (block on `Acquire`), never drop silently; emit over-limit metric. Independent per host.

## SR-5 — Shallow JS discovery (r5, C-8, FR-10)
- Parse `<script src>` from a fetched HTML response; GET those JS URLs **from approved hosts only**; third-party/unapproved ⇒ skip + log. No execution, no headless (A-6; dynamic JS = Post-MVP Playwright asset-discovery).
- Discovered JS cached as its own HAR entry; PS-009 signatures scan HTML + discovered JS bodies.

## SR-6 — Observation signatures (질문1=A; US-006/008-013/030-033, FR-03/05)
- **One signature = one CWE.** Each declares `CWE()` and judges a fetched response with **tight anchors** (FR-05); a benign/negative fixture ⇒ 0 findings, a positive fixture ⇒ 1 finding (PBT anchor property).
- Signatures run **independently** (R7): one failing signature ⇒ its CWE tool-failed; others unaffected.
- A signature may return `notApplicable=true` (FQ5) when the item is structurally inapplicable (e.g. no login form for a login-form CWE) — not an error, not a finding.
- The engine derives `OwnedCWEs()`; wiring feeds `findings.coverage.CWEOwnership` (`observation:CWE-615 → [CWE-615]`).

## SR-7 — Probe adapters (질문3=A; US-007, FR-04, SEC-10)
- Each wraps one **version-pinned** bundled CLI; `exec.CommandContext` with a timeout (NFR-12); parse stdout → findings (via `findings.normalize.RawFinding`).
- Parsing is **total**: malformed tool output ⇒ no panic; unparseable ⇒ the adapter returns an error (that item tool-failed) or an empty result — never a crash.
- Adapter declares `OwnedCWEs()`.

## SR-8 — Fault isolation & outcome (US-027, NFR-12)
- A checker/adapter failure never aborts the WorkItem's other checkers; it lands in `WorkItemOutcome.Failed` → its items tool-failed at accounting.
- `Check` is all-or-nothing (R7): `CheckResult` on success (findings + N/A CWEs), error otherwise.
- Every outbound HTTP + external-tool call has an explicit timeout.

## SR-9 — CheckResult → WorkItemOutcome aggregation (FQ2)
- Per WorkItem, the Executor merges each checker's `CheckResult`: `Findings` concatenated, `NotApplicable[checker] = result.NotApplicable`, failed checkers in `Failed`.
- `NotApplicable` (CWEs) is handed to `findings.CoverageAccountant`, which maps CWE→PS-ID to set item N/A.

## SR-10 — Evidence bounding (FR-12, SEC-03)
- Findings carry bounded evidence only (delegated to `findings.normalize`); `HAREntry.Body` never persisted as a finding; sensitive evidence redacted before logging.

---
## Property → rule (PBT, Code-Gen)
| Property | Rule |
|---|---|
| signature anchor tightness (pos/neg fixtures) | SR-6 |
| probe parser total (no panic) | SR-7 |
| fetch-once per URL | SR-2 |
| fail-closed (unapproved ⇒ 0 requests) | SR-3 |
