# Domain Entities — Unit `domain` (U0)

**Stage**: 🟢 CONSTRUCTION → `domain` → Functional Design (artifact 1/3) · **Date**: 2026-07-08
**Basis**: domain-functional-design-plan (Q1=B, Q2=A, Q3a=A, Q3b=Info, Q4=A, Q5=A, DQ1=A, DQ2=A) · requirements r4 §6/§7
**Nature**: pure contract — technology-agnostic types, no I/O. Every other unit implements against these.

---

## 1. Identifiers & enums

```go
type CheckerID string      // "observation:CWE-615" | "probe:humble" | "probe:httpx" | ...
type ScopeKind int         // URLScoped | HostScoped
type PSID string           // "PS-001" .. "PS-026"
type Framework string      // "KISA" (MVP); "OWASP", "CAPEC" (Post-MVP)
type DelegateTarget string // "ASM" | "Nuclei"
```

### 1.1 `Severity` (ordinal, total — Q3a)
```go
type Severity int
const ( SevInfo Severity = iota; SevLow; SevMedium; SevHigh; SevCritical ) // Info<Low<Medium<High<Critical
```
- **Ordered**: comparison is meaningful (used by DQ2 "max severity on collapse").
- **Total (US-016)**: `MapSeverity` is defined for every input; unmappable/unknown → **`SevInfo`** (Q3b — passive observation is recon-tier, so an unknown-severity finding defaults to informational rather than inflating risk).

### 1.2 `ItemStatus` (coverage, 4-state — Q4/US-020)
```go
type ItemStatus int
const ( StatusChecked ItemStatus = iota; StatusNotApplicable; StatusDelegated; StatusToolFailed )
```
Mutually exclusive per PS-ID (see business-rules §3 totality+disjointness).

---

## 2. CWE representation (Q2)

```go
type CWE string // canonical MITRE form, e.g. "CWE-540"
const NoCanonicalCWE CWE = "no-canonical-cwe" // reserved sentinel (FR-14; e.g. PS-025)
```
- Stored as string to match MITRE notation, mapping-table keys, and report readability.
- `NoCanonicalCWE` is a **valid** value a checker may emit when no clean CWE exists (A-3); the projection contract handles it explicitly (business-rules §4).

---

## 3. `Finding` — the emitted unit (CWE axis)

```go
type Finding struct {
    CWEs       []CWE      // ≥1; may be exactly [NoCanonicalCWE]. Normalized set (sorted, deduped) — see rules §1.
    TargetURL  string     // the URL/target the finding is about
    SourceTool string     // "observation:CWE-615" | "humble" | "httpx" | "testssl" | "dirhunt" | "changeme" | "corsy" | "subzy"
    Severity   Severity
    Evidence   Evidence   // bounded (FR-12)
}

type Evidence struct {
    Snippets []string // bounded count/length; redaction applied (SECURITY-03) before persistence/log
}
```

- **No PS-ID field** — a Finding is framework-agnostic at emission (US-014). PS-ID lives only in `Coverage`.
- **Identity (Q1=B, dedup key)** = `(normalized CWEs, TargetURL, SourceTool)`. See business-rules §1–§2.

---

## 4. `Coverage` — the accounting unit (PS-ID axis)

```go
type Coverage map[PSID]ItemRecord

type ItemRecord struct {
    Status ItemStatus
    Target DelegateTarget // set iff Status == StatusDelegated (FR-11); "" otherwise
}

// CoverageSpec is supplied from config (item set + delegations). The kernel does
// NOT hardcode the 26 KISA items — that set is customer-changeable, so it lives
// in config, keeping domain framework-agnostic. (Refactored to B on 2026-07-08.)
type CoverageSpec struct {
    Items       []PSID
    Delegations map[PSID]DelegateTarget
}
func (c Coverage) Validate(spec CoverageSpec) error
```
- Domain of the map = exactly `spec.Items` (KISA's PS-001..026 in MVP, but from config, not hardcoded).
- `StatusDelegated` requires the `Target` declared in `spec.Delegations` (e.g. PS-014→ASM; PS-005/006/026→Nuclei — all config).
- Invariant (business-rules §3): the four statuses partition `spec.Items`.
- *(B refactor)* `AllPSIDs()`, the `delegableItems` map, and `DelegationTarget()` were **removed** from the kernel — the item set + delegations are now config-driven via `CoverageSpec`. `DelegateTarget` + the `ASM`/`Nuclei` constants stay (chiller's fixed pipeline stages, not customer check-items).

---

## 5. `Checker` seam & `ScanContext` (Q4, Q5)

```go
// CheckResult (scan-FD FQ2): a checker's success output — findings + the CWEs it
// judged structurally not-applicable (N/A is first-class, not an error/finding).
type CheckResult struct {
    Findings      []Finding
    NotApplicable []CWE
}

type Checker interface {
    Check(ScanContext) (CheckResult, error) // all-or-nothing: CheckResult on success, error ⇒ its item(s) tool-failed (Q4/FQ2)
    Scope() ScopeKind
    ID() CheckerID
}

type ScanContext struct {
    Target       Target        // host / URL under test
    Response     *HAREntry     // non-nil for URL-scoped checkers (shared fetch, FR-02); nil for host-scoped
    DiscoveredJS []*HAREntry   // r4/C-8: statically <script src>-discovered first-party JS (approved hosts only)
}

type Target struct {
    URL    string
    Host   string
    Scheme string // "http" | "https"
}
```
- URL-scoped checkers (ObservationEngine, humble, httpx, Corsy, changeme) read `Response`/`DiscoveredJS`.
- Host-scoped checkers (testssl, dirhunt, subzy) use only `Target` and do their own protocol I/O (`Response` nil).

---

## 6. `HAREntry` — cached response subset (Q5, NFR-16)

```go
type HAREntry struct {
    URL       string
    Scheme    string        // "http" | "https"  (→ PS-003 plaintext login)
    Status    int
    Headers   []Header      // → humble items, PS-024, etc.
    Cookies   []Cookie      // → PS-015/016/017
    Body      []byte        // BOUNDED (max-body-size, NFR-16); HTML or JS
    FetchedAt Timestamp     // set by the fetcher; domain treats as opaque
}
type Header struct{ Name, Value string }
type Cookie struct{ Name, Value string; Attrs map[string]string } // Secure/HttpOnly/SameSite in Attrs
```
- A **subset** of full HAR — only fields the Observation signatures consume. Full-HAR fidelity is a non-goal.
- `Body` is bounded and transient (lives only in `ResponseCache`, wiped post-scan; never persisted as a finding, FR-12).

---

## 7. WorkItem (scheduling unit; defined here as a shared type, used by platform)

```go
type WorkItemID string
type WorkItem struct {
    ID      WorkItemID
    ScanID  ScanID
    Scope   ScopeKind
    Target  Target       // the single URL (URLScoped) or host (HostScoped) — C-6
    // URLScoped ⇒ one fetch shared by all URL-scoped checkers on this Target
}
type WorkItemOutcome struct {
    ID            WorkItemID
    Findings      []Finding
    Failed        []CheckerID          // checkers that errored → their PS-IDs become tool-failed
    NotApplicable map[CheckerID][]CWE  // (scan-FD FQ2) CWEs a checker judged N/A → mapped to N/A items
}
```

---

## 8. Entity relationships (contract-level)

```
Checker ──emits──▶ Finding (CWE axis) ─┐
                                        ├─▶ Normalizer→Dedup→Severity (findings unit)
WorkItemOutcome.Failed ─────────────────┘        │
                                                  ▼
                                         ComplianceMapper: CWE → PSID (FR-14)
                                                  │
Report = { []Finding (CWE) , Coverage (PSID) , mappingRevision }
```
- **Two axes never merge**: `Finding` carries CWE; `Coverage` carries PS-ID. The mapping (findings unit) is the only bridge.
- domain defines the *types*; the *algorithms* (dedup, mapping, accounting) are in the findings unit's Functional Design.
