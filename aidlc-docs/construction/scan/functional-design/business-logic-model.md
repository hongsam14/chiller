# Scan — Business Logic Model

**Stage**: 🟢 CONSTRUCTION → `scan` (U2) → Functional Design (artifact 3/3) · **Date**: 2026-07-08
**Purpose**: the acquisition→inspection flow, the checker seam realization, and how scan feeds findings.

---

## 1. Per-WorkItem flow

```
 domain.WorkItem (URL-scoped or host-scoped, from platform)
        │
        ▼  URL-scoped — Fetch+Cache phase (ordered, discovery is step ② INSIDE this phase):
   ① Fetcher.Fetch(mainURL) ─ Authorize (SR-3) ─ RateLimiter.Acquire (SR-4) ─ GET once ─▶ ResponseCache (HTML)
   ② Discoverer.Discover(target, html) (SR-5, approved hosts) ─▶ JS asset URLs
   ③ Fetcher.Fetch(each JS URL) ─ gate+limiter ─▶ ResponseCache (JS)
   ④ ─▶ ScanContext{Target, Response(HTML), DiscoveredJS(cached JS)}
        ▼  (Checking runs only after the fetch phase completes)
        │  run URL-scoped checkers independently (SR-8):
        ├─ ObservationEngine.Check ─(10 sigs, one CWE each, SR-6)─▶ CheckResult{Findings, N/A}
        ├─ humble.Check / httpx.Check (read cache) ───────────────▶ CheckResult
        └─ Corsy.Check / changeme.Check (own request) ────────────▶ CheckResult
        ▼  host-scoped WorkItem instead:
   testssl / dirhunt / subzy .Check (own protocol I/O, Response nil)
        ▼
   Executor aggregates → domain.WorkItemOutcome{Findings, Failed, NotApplicable} (SR-9)
        ▼
   (platform routes the outcome to findings: normalize→dedup→severity→persist; accountant uses Failed + NotApplicable)
```

## 2. Observation engine, worked (質問1=A, PS-009)
- Signatures for PS-009: `observation:CWE-615` (sensitive comments), `observation:CWE-540` (secrets in source), `observation:CWE-200` (info exposure) — one CWE each.
- Each runs over `Response.Body` + `DiscoveredJS[i].Body` (r5) with tight anchors: positive fixture → 1 finding; benign fixture → 0 (the PBT anchor property).
- Engine `OwnedCWEs()` = {CWE-615, CWE-540, CWE-200, …} → wiring builds `CWEOwnership{observation:CWE-615:[CWE-615], …}` that findings' boot completeness check validates against the mapping.

## 3. N/A path (FQ2, SR-9)
- e.g. the cookie-Secure signature (`observation:CWE-614`) on a response that sets no cookies → `notApplicable=true`.
- Engine aggregates → `CheckResult.NotApplicable = [CWE-614]` → `WorkItemOutcome.NotApplicable[observation:CWE-614] = [CWE-614]` → accountant maps CWE-614→PS-015 → `StatusNotApplicable` (only if structurally inapplicable, SR-6/質문5-findings).

## 4. What scan produces for findings (the hand-off)
- `[]domain.Finding` (CWE-keyed) via checkers → findings.normalize.
- `domain.WorkItemOutcome` (Findings/Failed/NotApplicable) → findings.CoverageAccountant.
- `CWEOwnership` (from the registry) → findings.coverage.New (boot).
- The authoritative CWE↔PS-ID mapping already lives in `configs/mappings/kisa.yaml`; scan's job is to emit the right CWEs and own them.

## 5. Boundaries & deferrals
- **Does not**: schedule/queue (platform), account/report (findings). scan = acquire + inspect + emit.
- **Deferred to Code Gen / later**: real per-tool output parsers (some stubbed, pinned per C-5); the CLI binaries themselves are bundled at Infrastructure/Build (SEC-10 pinning); ResponseCache Valkey impl at Infra (in-memory for tests now).
- **Next unit** (last): **platform** — FSM, WorkQueue, fan-out, ScanAPI; wires scan.Executor + findings pipeline.
