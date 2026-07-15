# Scan — Local Entities & Contracts

**Stage**: 🟢 CONSTRUCTION → `scan` (U2) → Functional Design (artifact 1/3) · **Date**: 2026-07-08
**Basis**: scan-functional-design-plan (질문1-5 = A) · domain contract (incl. FQ2 `CheckResult`/`WorkItemOutcome.NotApplicable`) · requirements r5.
Types owned by the scan unit. Consumes `domain.Checker`/`ScanContext`/`HAREntry`/`Target`/`WorkItem`/`Finding` (fixed).

---

## 1. Ingestion
```go
type ScanRequest struct {
    URLs     []string       // pre-collected target URLs (FR-01; no crawling)
    Approved []string       // per-scan approved targets/hosts (질문4)
    Options  ScanOptions    // throttle, checker selection
}
type ScanOptions struct {
    RatePerHost int          // NFR-13
    Burst       int
    MaxURLs     int          // input bound (SECURITY-05)
    Tools       []string     // optional checker allowlist
}
type URLSet struct{ URLs []domain.Target } // validated, in-scope
```

## 2. Safety (질문4/5) — isolated module (SECURITY-11)
```go
type ApprovalGate interface { Authorize(domain.Target) error } // fail-closed (SEC-15)
type RateLimiter interface { Acquire(host string) (release func(), err error) } // per-host token bucket
```
- Approval set = per-scan `Approved` ∩ optional global allowlist (질문4=A). Empty/absent global ⇒ request set governs; unapproved ⇒ reject before any outbound.
- RateLimiter: per-host token bucket (rate/burst from `ScanOptions`), over-limit delays + emits a metric (질문5=A).

## 3. Fetch + Cache (US-002, C-8)
**Timing**: the fetch phase is an ordered 4-step sequence — ① Fetch(main URL)→cache HTML, ② `Discoverer.Discover(target, html)`→JS URLs, ③ Fetch(each JS URL)→cache JS, ④ assemble `ScanContext`. **Discovery is step ②, *inside* the fetch phase** (its output feeds step ③); Checking runs only after ④. Static discoverer reads `html` (parse); Playwright discoverer uses `target` (loads the page itself).
```go
type Fetcher interface {
    Fetch(domain.Target) (*domain.HAREntry, error)   // once per URL, via gate+limiter → cache
}
// Discoverer is a swappable seam (NFR-02) for JS discovery mode:
//   StaticDiscoverer   (MVP)      — <script src> regex, approved-host only, no execution (C-8)
//   PlaywrightDiscoverer (Post-MVP) — Chromium network capture of requested JS URLs (r5 A-6, HC1=A); drops in unchanged.
type Discoverer interface { Discover(target domain.Target, html *domain.HAREntry) []domain.Target }
type ResponseCache interface {
    Put(scanID domain.ScanID, url string, e *domain.HAREntry) error
    Get(scanID domain.ScanID, url string) (*domain.HAREntry, bool)
    WipeScan(scanID domain.ScanID) error             // post-scan (FR-12/NFR-16)
}
```

## 4. Checking — the Checker seam (질문1/3)

### 4.1 Observation engine (in-process, URL-scoped)
```go
// One signature = one CWE (질문1=A). The engine derives CWEOwnership from the set.
type ObservationSignature interface {
    CWE() domain.CWE                                 // the single CWE this signature emits
    // Judge returns findings (issue present) or a not-applicable=true (structural N/A, FQ5).
    Judge(ctx domain.ScanContext) (findings []domain.Finding, notApplicable bool, err error)
}
type ObservationEngine struct{ sigs []ObservationSignature } // implements domain.Checker, Scope()==URLScoped
```
- 10 signatures (r5): `observation:CWE-…` for PS-003/008/009/010/011/012/013/018/022/023 items (a PS item with N CWEs → N signatures).
- `Check` runs each signature independently (R7 engine exception); aggregates into `CheckResult{Findings, NotApplicable}`; a signature error marks only its CWE tool-failed.
- Exposes `OwnedCWEs() []domain.CWE` so wiring builds `findings.coverage.CWEOwnership`.

### 4.2 Probe adapters (external CLI, 7) (질문3)
```go
type ProbeAdapter interface {
    domain.Checker          // Check(ScanContext) (CheckResult, error)
    ToolName() string
    PinnedVersion() string  // SECURITY-10
    OwnedCWEs() []domain.CWE
}
// exec.CommandContext(timeout NFR-12) → per-adapter parser → []domain.Finding (RawFinding→Finding via findings.normalize)
```
- humble/httpx: URL-scoped, read `ScanContext.Response` (cache reuse). testssl/dirhunt/subzy: host-scoped, own protocol I/O (Response nil). Corsy/changeme: URL-scoped, own request.
- Real per-tool output parsers are per-adapter; some ship as stubs this code-gen, pinned progressively (C-5).

### 4.3 Registry
```go
type CheckerRegistry interface {
    Register(domain.Checker) error
    List() []domain.Checker
    OwnershipForFindings() map[domain.CheckerID][]domain.CWE // → findings.coverage.CWEOwnership
}
```

## 5. WorkItem execution (bridge to platform)
scan does not schedule (platform does); it exposes an executor the worker calls per `domain.WorkItem`:
```go
type Executor interface {
    Run(domain.WorkItem, ResponseCache) domain.WorkItemOutcome
}
```
- URL-scoped WorkItem: fetch once (cache) → run URL-scoped checkers → aggregate findings + `NotApplicable map[CheckerID][]CWE` + `Failed`.
- Host-scoped WorkItem: run the one host-scoped checker.
- `NotApplicable` (CWEs) flows to `findings.CoverageAccountant` (translated CWE→PSID at accounting).
