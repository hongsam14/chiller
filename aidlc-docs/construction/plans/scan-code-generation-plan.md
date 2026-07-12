# Code Generation Plan — Unit `scan` (U2)

**Stage**: 🟢 CONSTRUCTION → `scan` → Code Generation (Part 1: Planning) · **Date**: 2026-07-08
**Single source of truth for scan code generation.**

**Unit context**
- Unit: U2 data plane (`internal/scan`) — acquire + inspect + emit. Stories US-002~013, 030~033.
- Depends on: `internal/domain` only (arch-check enforced). Emits `domain.Finding` + `domain.WorkItemOutcome`; exposes `CWEOwnership` for findings.
- Spec: `../scan/functional-design/{domain-entities,business-rules,business-logic-model}.md` (SR-1..SR-10, 질문1-5=A).

**Reuse discipline**: consumes domain (`Checker`/`CheckResult`/`ScanContext`/`HAREntry`/`Target`/`WorkItem`/`WorkItemOutcome`/`Finding`) and findings' `RawFinding` shape — no reimplementation. **No new external dep** (stdlib net/http; `<script src>` via a conservative regex — x/net/html a documented future improvement; token bucket in-house).

**Real vs deferred (honest scope)**
- **Real + tested now**: ingestion, safety (gate + limiter), fetch (in-memory cache, HTTP fetch via httptest, script discovery), Observation engine + **the 10 signatures with tight anchors** (pure over HAREntry — fully implementable), registry, executor.
- **Framework + stub parsers**: the 7 Probe adapters — exec.CommandContext scaffolding + parse hook tested with a fake command; tool-specific output parsers marked TODO (need the real bundled CLIs, pinned per C-5/Infra).
- **Deferred (documented)**:
  - Valkey ResponseCache impl (Infra); CLI binary bundling + version pinning (Infra/Build); real per-tool parsers.
  - **Post-MVP: Playwright (Chromium) network-capture asset-discovery mode (r5 A-6 / HC1=A).** A second `Discoverer` alongside the MVP static `<script src>` path: load the page once (depth-0, no navigation/interaction), capture the **call traffic** it actually requests via `page.on("request"/"response")`, take the **requested JS URLs** (network log, not a DOM dump), GET those from approved hosts (FR-10), feed the bodies to the PS-009 signatures. Bundler-agnostic (catches code-split/lazy/runtime-injected JS the static path misses). Deliberately steps outside pure-passive (executes page JS) → an opt-in mode, and adds a headless-browser dependency at Infra. Design in scan FD SR-5; **not implemented in this MVP code gen** — the MVP dynamic-JS gap is the documented §2.2 Known limitation. Seam: the `Discoverer` interface is shaped so this mode drops in without touching the checkers.

---

## Steps
### Step 1 — Ingestion `[ ]` (US-003, SR-1)
- [ ] `internal/scan/ingest/` — `URLIngestor.Ingest(ScanRequest) (URLSet, error)`: validate URL format/host-in-approved/list-bound/option-range; fail-closed. + test.

### Step 2 — Safety `[ ]` (US-004/005, SR-3/4, SEC-11)
- [ ] `internal/scan/safety/` — `ApprovalGate` (effective = request ∩ global allowlist; `Authorize` fail-closed) + `RateLimiter` (per-host token bucket, in-house) + tests.

### Step 3 — Fetch + Cache `[ ]` (US-002, SR-2/5)
The **Fetch+Cache phase** runs as an ordered 4-step sequence per URL-scoped WorkItem and produces the fully-populated cache **before** Checking. Discovery is step ②, *inside* this phase (its output triggers the step-③ fetches).
1. `Fetcher.Fetch(mainURL)` — Authorize→Acquire→GET once → cache HTML.
2. `Discoverer.Discover(target, html)` — derive JS asset URLs (approved hosts).
3. `Fetcher.Fetch(each JS URL)` — same gate+limiter → cache JS.
4. Assemble `domain.ScanContext{Response: HTML, DiscoveredJS: [cached JS]}` → hand to Checking (Step 4/7).

- [ ] `internal/scan/fetch/` — `ResponseCache` iface + `InMemory`; `Fetcher.Fetch` (steps ①/③).
- [ ] **`Discoverer` seam** (step ②) — `Discoverer interface { Discover(target domain.Target, html *domain.HAREntry) []domain.Target }`, swappable (NFR-02). MVP impl = `StaticDiscoverer` (uses `html`: `<script src>` regex, approved-host filter, **no execution**). Post-MVP `PlaywrightDiscoverer` (uses `target`: Chromium loads the page, captures requested JS URLs, r5 A-6) implements the same seam and drops in without touching Fetcher/checkers — **not built here**. *(Playwright's page load overlaps step ①; whether it replaces the plain main-fetch to avoid a double fetch is a wiring detail deferred with the Post-MVP impl.)* MVP ships static + the documented dynamic-JS gap (§2.2 Known limitation). Tests via `httptest`.

### Step 4 — Observation engine + signatures `[ ]` (US-006/008-013/030-033, SR-6, P: anchors)
- [ ] `internal/scan/checking/observation/` — `ObservationSignature` iface (one CWE each) + `ObservationEngine` (implements `domain.Checker`, aggregates CheckResult, per-signature isolation, `OwnedCWEs`). 10 signatures with tight anchors over HAREntry (cookies/headers/body/url/well-known-paths). Tests: positive+negative fixtures per signature (anchor property).

### Step 5 — Probe adapters `[ ]` (US-007, SR-7, SEC-10)
- [ ] `internal/scan/checking/probe/` — `ProbeAdapter` framework (`exec.CommandContext` timeout, parse hook, `OwnedCWEs`, `PinnedVersion`) + the 7 concrete adapters (parsers TODO-stubbed where tool output needed). Test the framework with a fake command.

### Step 6 — Registry `[ ]` (SR / NFR-02)
- [ ] `internal/scan/checking/registry/` — `CheckerRegistry.Register/List/OwnershipForFindings` (→ `map[CheckerID][]CWE`). + test.

### Step 7 — Executor `[ ]` (US-027, SR-8/9)
- [ ] `internal/scan/exec/` — `Executor.Run(WorkItem, ResponseCache) WorkItemOutcome`: fetch (URL-scoped) → run checkers independently → aggregate Findings/Failed/NotApplicable. Test with fake checkers incl. N/A + failure.

### Step 8 — Summary `[ ]`
- [ ] `aidlc-docs/construction/scan/code/business-logic-summary.md`.

## Verification
`go build ./... · go vet ./... · go test ./internal/scan/... · make arch-check` (scan imports domain only).

## Checkboxes
- [x] Step 1 · [x] Step 2 · [x] Step 3 · [x] Step 4 · [x] Step 5 · [x] Step 6 · [x] Step 7 · [x] Step 8 — **build/vet/test/gofmt/arch-check green + cross-unit wiring test**
