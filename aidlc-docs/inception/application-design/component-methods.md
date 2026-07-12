# Component Methods — chiller

**Stage**: INCEPTION → Application Design (artifact 2/5) · **Date**: 2026-07-08
**Scope**: method **signatures** + I/O types + high-level purpose. **Business rules (anchors, dedup algorithm, FSM transition table, projection contents) → Functional Design.**
Signatures are illustrative Go (NFR-01: interface-forced construction, DI). `ctx context.Context` omitted for brevity but assumed on every I/O method.

---

## Shared domain types (defined once — NFR-03)

```go
type CheckerID string
type ScopeKind int // URLScoped | HostScoped
type Severity int  // Info | Low | Medium | High | Critical (total, US-016)
type Framework string // "KISA" | "OWASP" | "CAPEC"
type PSID string      // "PS-001".."PS-026"
type DelegateTarget string // "ASM" | "Nuclei"

type Finding struct {
    CWEIDs     []string // ≥1, or the sentinel NoCanonicalCWE (FR-14)
    Severity   Severity
    Evidence   string   // bounded snippet only (FR-12)
    SourceTool string   // observation:<id> | humble | httpx | testssl | dirhunt | changeme | corsy | subzy
    TargetURL  string
}

type ScanContext struct {
    Target   Target          // host / URL
    Response *ResponseHandle // non-nil only for URL-scoped checkers (from ResponseCache)
}

type ItemStatus int // Found | NotApplicable | Delegated | ToolFailed (US-020)
type Coverage map[PSID]struct {
    Status ItemStatus
    Target DelegateTarget // set iff Status==Delegated (FR-11)
}
```

---

## Platform

### ScanOrchestrator
```go
type ScanOrchestrator interface {
    CreateScan(req ScanRequest) (ScanID, error)
    StartScan(id ScanID) error          // FSM created→running; fan out WorkItems (US-024,026)
    StopScan(id ScanID) error           // FSM →stopped
    GetStatus(id ScanID) (ScanStatus, error)
    ResumeScan(id ScanID) error         // re-enqueue only unfinished WorkItems (US-025)
}
func NewScanOrchestrator(TaskStore, WorkQueue, CheckerRegistry, ResponseCache,
                         FindingsPipeline, ProgressReporter) ScanOrchestrator
```
- `FindingsPipeline` = the composed Normalizer→Dedup→Severity→Compliance→Accountant→ReportBuilder entry (see services.md).

### TaskStore
```go
type TaskStore interface {
    SaveScan(Scan) error
    LoadScan(ScanID) (Scan, error)
    CompareAndSetState(id ScanID, from, to FSMState) (bool, error) // CAS (US-025)
    MarkWorkItemDone(id ScanID, wi WorkItemID, outcome WorkItemOutcome) error
    ListUnfinished(id ScanID) ([]WorkItem, error)                  // resume source of truth
}
```

### WorkQueue
```go
type WorkQueue interface {
    Enqueue(WorkItem) error
    Dequeue() (WorkItem, error) // bounded in-flight (US-026)
    Ack(WorkItemID) error
    Nack(WorkItemID) error      // failure → tool-failed path (US-027)
}
```

### ProgressReporter
```go
type ProgressReporter interface {
    RecordProgress(id ScanID, checker CheckerID, done, total int)
    Health() HealthReport // shallow+deep: store/queue/cache/tools (NFR-11)
    Emit(evt ObservabilityEvent) // OTel
}
```

### ScanAPI
```go
type ScanAPI interface { // versioned contract (P4)
    SubmitScan(AuthCtx, ScanRequest) (ScanID, error)   // authz deny-by-default (SEC-08)
    GetScanStatus(AuthCtx, ScanID) (ScanStatus, error)
    GetReport(AuthCtx, ScanID) (Report, error)
}
func NewScanAPI(ScanOrchestrator, ReportBuilder, AuthN) ScanAPI
```

---

## Scan (data plane)

### URLIngestor
```go
type URLIngestor interface {
    Ingest(req ScanRequest) (URLSet, error) // validation; reject pre-outbound (SEC-05,15)
}
```

### ApprovalGate
```go
type ApprovalGate interface {
    Authorize(t Target) error // fail-closed; audit who-scanned-what (SEC-11,14,15)
}
```

### RateLimiter
```go
type RateLimiter interface {
    Acquire(t Target) (release func(), err error) // per-target (NFR-13)
}
```

### Fetcher
```go
type Fetcher interface {
    Fetch(url URL) (ResponseHandle, error)        // once per URL → ResponseCache (US-002, FR-02)
    DiscoverScripts(html ResponseHandle) []URL    // r4/C-8: parse <script src>, approved-host only (FR-10/GC2); no execution
}
func NewFetcher(ApprovalGate, RateLimiter, ResponseCache) Fetcher
// Non-goal: no headless rendering (A-6). Dynamic chunks = Post-MVP static manifest enumeration.
```

### ResponseCache (NFR-16)
```go
type ResponseCache interface {
    Put(scanID ScanID, url URL, entry HAREntry) error // encrypted, bounded, TTL
    Get(scanID ScanID, url URL) (HAREntry, bool)
    WipeScan(scanID ScanID) error                     // post-scan wipe (FR-12 boundary)
}
```

### Checker seam
```go
type Checker interface {
    Check(ScanContext) ([]Finding, error) // emits CWE-keyed, framework-agnostic
    Scope() ScopeKind                     // URLScoped | HostScoped (C-6)
    ID() CheckerID
}
type CheckerRegistry interface {
    Register(Checker) error
    List() []Checker
    Get(CheckerID) (Checker, bool)
}
```

### ObservationEngine (implements Checker; Family A)
```go
type ObservationSignature interface { // one per item (PS-003/008/009/010/011/012/013/018/022/023)
    ItemID() CheckerID
    Judge(*ResponseHandle) ([]Finding, error) // tight anchors → Functional Design
}
func NewObservationEngine(sigs []ObservationSignature) Checker // Scope()==URLScoped, in-process-parallel (EC4)
```

### ProbeAdapter (implements Checker; Family B ×7)
```go
type ProbeAdapter interface { // Checker + tool metadata
    Checker
    ToolName() string
    PinnedVersion() string // SECURITY-10
}
func NewHumbleAdapter(ResponseCache) ProbeAdapter   // URLScoped, reads cache
func NewHttpxAdapter(ResponseCache) ProbeAdapter     // URLScoped, reads cache
func NewCorsyAdapter() ProbeAdapter                  // URLScoped, own request
func NewChangemeAdapter() ProbeAdapter               // URLScoped, own request
func NewTestsslAdapter() ProbeAdapter                // HostScoped, bypass cache (EC3)
func NewDirhuntAdapter() ProbeAdapter                // HostScoped, bypass cache
func NewSubzyAdapter() ProbeAdapter                  // HostScoped, bypass cache
```

---

## Findings (data plane)

```go
type Normalizer interface {
    Normalize(raw []byte, source string) ([]Finding, error) // total parse (US-014)
}
type Deduplicator interface {
    Dedup([]Finding) []Finding // idempotent, order-independent (US-015)
}
type SeverityMapper interface {
    MapSeverity(toolSev string, source string) Severity // total (US-016)
}
type ComplianceMapper interface { // FR-14 mapping layer
    Project(cweIDs []string, fw Framework) []FrameworkItem // totality: ≥1 item or no-canonical marker
    Revision() string
}
func NewComplianceMapper(table MappingTable) ComplianceMapper // declarative file (Q5=A)

type CoverageAccountant interface {
    Account(findings []Finding, outcomes []WorkItemOutcome) Coverage // 26 PS-ID, 4-state, targets (US-020,021)
}
type ReportBuilder interface {
    Build(scanID ScanID) (Report, error) // findings(CWE) + coverage(PS-ID) + mapping revision (US-019)
}
type FindingsStore interface {
    SaveFindings(scanID ScanID, []Finding) error // bounded evidence, encrypted (FR-12, SEC-01)
    LoadFindings(scanID ScanID) ([]Finding, error)
}
```

---

**Note**: all constructors take dependencies as **interfaces** (NFR-01 DI). No component news up another concrete component. Detailed method bodies, error taxonomies, FSM transition tables, dedup/severity/anchor rules, and per-tool parsing are authored at **Functional Design** (per unit).
