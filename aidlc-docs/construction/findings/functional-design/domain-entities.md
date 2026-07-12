# Findings — Local Entities & Config

**Stage**: 🟢 CONSTRUCTION → `findings` (U3) → Functional Design (artifact 1/3) · **Date**: 2026-07-08
**Basis**: findings-functional-design-plan (FQ1=A CWE-ownership, FQ1b=A, FQ2=A StatusChecked, FQ3=A, FQ4=B, FQ5=A) · domain contract (r1-r10) · requirements r5.
**Nature**: structures owned by the findings unit. Consumes `domain.Finding`/`Coverage`/`WorkItemOutcome`; does not redefine them.

---

## 1. Ownership registry (FQ1 / FQ1b / FQ6) — the accounting substrate

chiller classifies findings on **two axes**:
- **weakness axis (CWE)** — the default; most findings are a weakness. This is the canonical identity (C4).
- **attacker axis (ATT&CK technique)** — for items that are not a code/config weakness but an adversary-technique-enabling condition (e.g. PS-025 Subdomain Takeover ≈ ATT&CK **T1584.001**). These carry `NoCanonicalCWE` on the weakness axis and their real classification on the attacker axis (FQ6=A).

Coverage is derived from **what each checker checks** (FQ1), keyed on the weakness axis where possible, with an explicit **attacker-axis** table for the rest (renamed from "non-canonical" per user — positive attacker-perspective framing).

```go
// CWEOwnership: a checker's identity is the set of CWEs it can emit (generic,
// framework-agnostic). Example: observation:CWE-615 → {CWE-615};
// humble → {CWE-319, CWE-614, CWE-1004, CWE-1275, CWE-1021}.
type CWEOwnership map[domain.CheckerID][]domain.CWE

// AttackTechnique classifies items on the attacker axis — those with no canonical
// CWE. It supplies the owning checker (for accounting, FQ1b) and the ATT&CK
// technique labels (for the report, FQ6). e.g.
//   PS-025 → { Owner: "probe:subzy", Techniques: ["T1584.001"] }.
type AttackTechnique struct {
    Owner      domain.CheckerID
    Techniques []string // MITRE ATT&CK technique IDs
}
type AttackTechniqueTable map[domain.PSID]AttackTechnique
```

`domain.NoCanonicalCWE` (weakness axis) marks "no CWE"; the `AttackTechniqueTable` (attacker axis) then gives that item its real classification. The two are complementary layers, not a rename of each other.

### 1.1 CoverageSpec (config-driven — B refactor) — the item set + delegations
The KISA item set and its delegations are **customer-changeable**, so they live in config, not the kernel (`domain.CoverageSpec`, not hardcoded). The findings config declares them:
```yaml
# configs/coverage/kisa.yaml  (or a section of the mapping file)
items: [PS-001, PS-002, ..., PS-026]     # the authoritative accounting set
delegations: { PS-014: ASM, PS-005: Nuclei, PS-006: Nuclei, PS-026: Nuclei }
```
Loaded into a `domain.CoverageSpec{Items, Delegations}` at boot; the accountant passes it to `domain.Coverage.Validate(spec)`. Changing the checked items = editing this config, never the kernel.

## 2. Mapping table (FQ3, FR-14) — CWE → framework items

Declarative, versioned file (`configs/mappings/kisa.yaml` for MVP). CWE-keyed:

```yaml
version: "kisa-2026-07-08"
# weakness axis: CWE → framework items
map:
  CWE-615: { KISA: [PS-009] }
  CWE-540: { KISA: [PS-009] }
  CWE-200: { KISA: [PS-009] }
  CWE-1004: { KISA: [PS-016] }
  CWE-614: { KISA: [PS-015] }
  CWE-1275: { KISA: [PS-017] }
  no-canonical-cwe: { KISA: [] }   # explicit empty — never silence (R8)
# attacker axis: items with no canonical CWE, classified by ATT&CK technique (FQ6)
attackTechnique:
  PS-025:
    owner: probe:subzy
    ATTACK: [T1584.001]            # Compromise Infrastructure: Domains
    CAPEC: []
```
- Loaded into `MappingTable` + `AttackTechniqueTable` at boot. `Project(cwe, framework) → []item`; absent framework key ⇒ empty (R10).
- Frameworks: **KISA** (MVP) · **OWASP, CAPEC, ATT&CK** (Post-MVP projections). Adding any leaves KISA coverage bit-identical (P6).
- The `attackTechnique` section is the attacker axis (FQ6=A) — PS-ID-keyed because these items have no CWE to key on; carries the owner (accounting) + ATT&CK labels (report).
- *(Indicative rows; authoritative table pinned during this unit's construction — constraint C-5.)*

```go
type MappingTable struct {
    Version string
    // cwe → framework → []frameworkItem (PSID for KISA, "A0x" for OWASP, ...)
    m map[domain.CWE]map[domain.Framework][]string
}
func (t *MappingTable) Project(cwes []domain.CWE, fw domain.Framework) []string
func (t *MappingTable) Revision() string
```

## 3. Severity config (FQ4=B) — per-tool tables in a file

Per user (FQ4=B) severity mapping lives in a **config file**, not code:

```yaml
# configs/severity.yaml
tools:
  testssl:   { LOW: SevLow, MEDIUM: SevMedium, HIGH: SevHigh, CRITICAL: SevCritical }
  humble:    { warning: SevMedium, info: SevInfo }
  # ...
default: SevInfo    # R4 totality: any unmapped tool severity → Info (Q3b, recon-tier)
```
```go
type SeverityConfig struct {
    perTool map[string]map[string]domain.Severity
    // unmapped ⇒ domain.SevInfo (R4)
}
func (c *SeverityConfig) Map(rawSeverity, sourceTool string) domain.Severity
```

## 4. Report (US-019) — the two-axis output

```go
type Report struct {
    ScanID          domain.ScanID
    Findings        []domain.Finding // CWE axis (issues)
    Coverage        domain.Coverage  // PS-ID axis (26 accounted, StatusChecked/NA/Delegated/ToolFailed)
    MappingRevision string           // which mapping produced the framework labels
    // FrameworkView is a *projection* (labels), never the source of Coverage (P6):
    FrameworkLabels map[domain.Framework]map[string][]int // framework → item → finding indices
}
```
Both axes are present and independent: `Coverage` from ownership (§1), `FrameworkLabels` from projection (§2).

## 5. Required domain addition (apply at scan-unit construction)

FQ5=A needs checkers to signal **not-applicable** (not an error, not a finding). `domain.WorkItemOutcome` currently carries `Findings` + `Failed`; it needs a third channel:
```go
// (to add to domain.WorkItemOutcome when the scan unit — the producer — is built)
NotApplicable []domain.CheckerID  // checkers that ran but the item structurally doesn't apply
```
Flagged here (findings is the *consumer*); the producer (scan) is built later, so the domain edit is batched with it. Until then the accountant treats a ran checker as `StatusChecked`.
