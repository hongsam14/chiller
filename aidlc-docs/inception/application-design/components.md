# Components — chiller (passive-scanner engine)

**Stage**: INCEPTION → Application Design (artifact 1/5) · **Date**: 2026-07-08
**Basis**: `../requirements/requirements.md` (r3) · `../user-stories/stories.md` · `../plans/application-design-plan.md` §H + clarifications (Q/AC/BC/DC/EC)
**Scope**: high-level component identification, responsibilities, interfaces. **Detailed business logic → Functional Design (per-unit, CONSTRUCTION).**

---

## 0. Design invariants (from requirements/stories — not re-decided here)

- **3 bounded contexts** = Go top-level packages: `platform/` (control plane) · `scan/` + `findings/` (data plane).
- **Unidirectional dependency**: `platform → {scan, findings}`. Data plane MUST NOT import `platform` (NFR-02 checker seam).
- **Interface-forced construction (NFR-01)**: every component is an interface with a `New…()` constructor that takes its dependencies as interfaces (DI). No concrete cross-component references.
- **CWE = canonical finding identity; PS-ID = accounting axis** (FR-14). Checkers emit CWE, never PS-ID.
- **Deployment**: single image, `--role=manager|worker` (A-1). `manager` wires ScanAPI + ScanOrchestrator; `worker` wires the Checker registry + data-plane executors. Both share the same component definitions.

Legend: **[C]** control plane · **[D]** data plane · `iface` = interface seam.

---

## 1. Platform (control plane) — `platform/`

### 1.1 `ScanOrchestrator` [C] `iface`
- **Purpose**: the control plane. Drives a scan through its FSM and sequences the pipeline; owns *when/how*, never *what* (no domain logic).
- **Responsibilities**:
  - Create/start/stop/status/resume a Scan via the FSM (US-024).
  - Fan out a Scan into WorkItems and enqueue them (US-026, C-6).
  - On WorkItem completion, route emitted findings into the Findings pipeline.
  - Isolate WorkItem failures — one failure never aborts the scan; mark the item `tool-failed` (US-027).
  - Drive resume: consult the WorkItem completion ledger, re-enqueue only unfinished items (US-025).
- **Interface**: `CreateScan`, `StartScan`, `StopScan`, `GetStatus`, `ResumeScan`.
- **Depends on** (iface): `TaskStore`, `WorkQueue`, `CheckerRegistry`, `ResponseCache`, `Normalizer`→…→`ReportBuilder` (Findings pipeline entry), `ProgressReporter`.

### 1.2 `TaskStore` [C] `iface`
- **Purpose**: persistence of the Scan(Task) aggregate + WorkItem completion ledger + FSM state, with compare-and-set for safe concurrent resume.
- **Responsibilities**: persist/load Scan aggregate; atomic FSM transitions (CAS); record per-WorkItem completion (the resume source of truth, US-025); expose coverage state for the report.
- **Interface**: `SaveScan`, `LoadScan`, `CompareAndSetState`, `MarkWorkItemDone`, `ListUnfinished`.
- **Impl note**: repository seam; MongoDB default, swappable (NFR-04). Same logical store as findings, separate collection (Q6=A).

### 1.3 `WorkQueue` [C] `iface`
- **Purpose**: distribute WorkItems to workers with bounded concurrency.
- **Responsibilities**: enqueue/dequeue WorkItems; at-least-once delivery; bounded in-flight count (US-026). Durability of *resume* lives in TaskStore, not here (the queue only distributes).
- **Interface**: `Enqueue`, `Dequeue`, `Ack`, `Nack`.
- **Impl note**: Redis/Valkey (asynq-style) default; in-memory impl for tests (Q3=A). Cloud-agnostic (no SQS).

### 1.4 `ProgressReporter` [C] `iface`
- **Purpose**: observability of control + data paths.
- **Responsibilities**: per-checker progress metric (US-028); OTel metrics/logs/traces; shallow + deep health checks (store / queue / ResponseCache / external-tool availability, NFR-11).
- **Interface**: `RecordProgress`, `Health`, `Emit`.

### 1.5 `ScanAPI` [C] `iface`
- **Purpose**: the single external contract surface (P4, external uncontrolled consumers).
- **Responsibilities**: accept scan requests (US-001), expose status + merged report (US-023); authn/authz deny-by-default (SECURITY-08); set own security headers / dogfood (SECURITY-04); **versioned** contract.
- **Interface**: `SubmitScan`, `GetScanStatus`, `GetReport`.
- **Depends on** (iface): `ScanOrchestrator`, `ReportBuilder`.

---

## 2. Scan (data plane) — `scan/`

### 2.1 `URLIngestor` [D] `iface`
- **Purpose**: turn a raw scan request into a validated in-scope URL set.
- **Responsibilities**: input validation — URL format, host allowlist, list-size bounds, option ranges (US-003, SECURITY-05); reject before any outbound request (fail-closed, SECURITY-15). No crawling (FR-01).
- **Interface**: `Ingest(request) (URLSet, error)`.

### 2.2 `ApprovalGate` [D] `iface`
- **Purpose**: the safety boundary — nothing goes out to an unapproved target.
- **Responsibilities**: check every target against the pre-approval list *before* any outbound request; fail-closed reject (US-004, FR-10, SECURITY-11/15); append-only audit of who scanned what (SECURITY-14).
- **Interface**: `Authorize(target) error`.
- **Isolation note**: security-critical — lives in its own module (SECURITY-11), in the `Safety` subdomain.

### 2.3 `RateLimiter` [D] `iface`
- **Purpose**: protect scanned hosts from chiller-as-DoS.
- **Responsibilities**: per-target outbound rate limiting; suppress/delay over-limit requests; expose over-limit metric (US-005, NFR-13).
- **Interface**: `Acquire(target) (release func, error)`.

### 2.4 `Fetcher` [D] `iface`
- **Purpose**: fetch a URL's HTML response **once**, populate the ResponseCache, and perform shallow static JS discovery.
- **Responsibilities**: single fetch per URL through ApprovalGate + RateLimiter; write the response as a HAR entry into ResponseCache; return the cached handle (US-002, FR-02). *(r4, C-8)* parse `<script src>` from a fetched HTML response and, for **approved-host** JS URLs only (FR-10/GC2), GET them (no execution) and cache them as their own HAR entries; third-party/unapproved-host scripts are skipped+logged.
- **Interface**: `Fetch(ctx, url) (ResponseHandle, error)` · `DiscoverScripts(html ResponseHandle) []URL` *(r4, approved-host-filtered)*.
- **Depends on** (iface): `ApprovalGate`, `RateLimiter`, `ResponseCache`.
- **Non-goal**: no JS execution, no headless rendering (A-6); dynamic code-split chunks are Post-MVP static manifest enumeration.

### 2.5 `ResponseCache` [D] `iface` *(r3, NFR-16, EC1-4)*
- **Purpose**: transient cross-family fetch reuse.
- **Responsibilities**: store/read fetched responses as **HAR (or subset)** entries keyed by URL; **scan-scoped TTL**; **wipe at scan completion**; **encrypted at rest** (SECURITY-01); **bounded max-body-size**. **Not** findings persistence (FR-12 boundary). Loss ⇒ re-fetch, never data loss.
- **Interface**: `Put(scanID, url, harEntry)`, `Get(scanID, url) (harEntry, bool)`, `WipeScan(scanID)`.
- **Impl note**: Valkey-backed; only HTML-fetching URL-scoped checkers use it (Observation, humble, httpx); testssl/subzy/dirhunt bypass (EC3, C-7).

### 2.6 `Checker` [D] `iface` — the plugin seam (NFR-02, Q4=A)
- **Purpose**: one uniform seam for both checker families so tools are added/removed without touching the orchestrator.
- **Interface**: `Check(ScanContext) ([]Finding, error)` · `Scope() ScopeKind` (`URLScoped|HostScoped`, C-6) · `ID() CheckerID`.
- **`ScanContext`** (value type): carries `target` and, for URL-scoped checkers, the shared cached `ResponseHandle`. URL-scoped checkers read the cache; host-scoped checkers use only `target` and do their own protocol I/O.
- **`CheckerRegistry`** `iface`: `Register`, `List`, `Get` — the orchestrator sees only the registry, never concrete checkers.

#### Family A — `ObservationEngine` [D] (in-process native, URL-scoped)
- **Purpose**: judge the 10 observation items from the shared cached response (FR-03/FR-05).
- **Structure**: hosts a set of registered `ObservationSignature` functions — one per item: PS-003, 008, 009, 010, 011, 012, 013, 018, 022, 023. Each is a tight-anchor check over the HAR response (headers/cookies/body/URL). *(r4)* the PS-009 signature scans both the HTML body and any statically-discovered JS bodies (C-8) for sensitive comments/secrets (CWE-615/540/200). Detailed anchors → Functional Design.
- **Implements** `Checker` (one `ObservationEngine` per URL-scoped WorkItem, running its signatures in-process-parallel, EC4).

#### Family B — `ProbeAdapter` [D] `iface` ×7 (external CLI)
- **Purpose**: wrap one version-pinned external CLI tool, standardizing input + output parsing (FR-04).
- **Adapters**: `humble` (URL-scoped, 001/015/016/017/024) · `httpx` (URL-scoped, 007/020) · `testssl` (host-scoped, 002) · `dirhunt` (host-scoped, 004) · `changeme` (URL-scoped, 019) · `Corsy` (URL-scoped, 021) · `subzy` (host-scoped, 025).
- **Implements** `Checker`. URL-scoped HTML-consuming adapters (humble/httpx) may read ResponseCache; protocol-specific ones (testssl/dirhunt/subzy) bypass (EC3). Parsing is total — malformed output ⇒ no panic, surfaces as `tool-failed` (US-007, US-027).

---

## 3. Findings (data plane) — `findings/`

### 3.1 `Normalizer` [D] `iface`
- **Purpose**: heterogeneous checker output → one **CWE-keyed** `Finding` (FR-06, NFR-03).
- **Responsibilities**: parse each source tool's output to a Finding with `cwe_ids` (or `no-canonical-cwe`); framework-agnostic at emission (no PS-ID); total parsing (US-014).
- **Interface**: `Normalize(rawToolOutput, source) ([]Finding, error)`.

### 3.2 `Deduplicator` [D] `iface`
- **Purpose**: no item double-judged across checkers (FR-06).
- **Responsibilities**: idempotent, order-independent dedup of the finding set (US-015).
- **Interface**: `Dedup([]Finding) []Finding`.

### 3.3 `SeverityMapper` [D] `iface`
- **Purpose**: total mapping of tool severities to one valid scale (US-016).
- **Interface**: `MapSeverity(toolSeverity, source) Severity`.

### 3.4 `ComplianceMapper` [D] `iface` — the FR-14 mapping layer
- **Purpose**: project CWE onto compliance frameworks via a **declarative versioned table** (Q5=A).
- **Responsibilities**: load the mapping file; `project(cwe, framework) → items | no-canonical-cwe`; report the mapping revision; MVP = KISA only; OWASP/CAPEC = Post-MVP tables (US-017, US-018, NFR-15). Checkers stay framework-ignorant.
- **Interface**: `Project(cweIDs, framework) []FrameworkItem` · `Revision() string`.

### 3.5 `CoverageAccountant` [D] `iface`
- **Purpose**: the accounting axis — classify all 26 PS-IDs (US-020).
- **Responsibilities**: per-item status ∈ `{found, not-applicable, delegated, tool-failed}`; `delegated` carries target ∈ `{ASM, Nuclei}` (US-021, FR-11); enforce totality + pairwise-disjointness (§7 property); never let projection move accounting (US-018 property).
- **Interface**: `Account(findings, workItemOutcomes) Coverage`.

### 3.6 `ReportBuilder` [D] `iface`
- **Purpose**: merge findings (CWE axis) + coverage (PS-ID axis) + mapping revision into one report (US-019).
- **Interface**: `Build(scanID) Report`.

### 3.7 `FindingsStore` [D] `iface`
- **Purpose**: persist findings with bounded evidence, encrypted (US-022, FR-12, SECURITY-01).
- **Responsibilities**: store Findings (bounded snippet only — no full bodies); encrypted at rest; repository seam swappable (NFR-04). Same logical store as TaskStore, separate collection.
- **Interface**: `SaveFindings`, `LoadFindings`.

---

## 4. Component ↔ story coverage (completeness check, D-11)

| Story | Component(s) |
|---|---|
| US-001, US-023 | ScanAPI |
| US-002 | Fetcher, ResponseCache |
| US-003 | URLIngestor |
| US-004 | ApprovalGate |
| US-005 | RateLimiter |
| US-006 | ObservationEngine, Checker/CheckerRegistry |
| US-007 | ProbeAdapter ×7, Checker |
| US-008~013, US-030~033 | ObservationEngine (per-signature) |
| US-014 | Normalizer |
| US-015 | Deduplicator |
| US-016 | SeverityMapper |
| US-017, US-018 | ComplianceMapper |
| US-019 | ReportBuilder |
| US-020, US-021 | CoverageAccountant |
| US-022 | FindingsStore |
| US-024, US-025, US-026, US-027 | ScanOrchestrator, TaskStore, WorkQueue |
| US-028 | ProgressReporter |
| US-029 | (deploy/infra — NFR/Infra Design, not an app component) |

**Every non-deferred story maps to ≥1 component.** US-029 (blue/green + rollback) is deployment, handled at NFR/Infrastructure Design.
