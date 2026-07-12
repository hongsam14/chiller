# Findings — Business Logic Model

**Stage**: 🟢 CONSTRUCTION → `findings` (U3) → Functional Design (artifact 3/3) · **Date**: 2026-07-08
**Purpose**: the pipeline data flow, the CWE-ownership coverage model (FQ1/FQ1b), and how P1–P7 are realized.

---

## 1. Pipeline (per WorkItemOutcome, then per scan)

```
 WorkItemOutcome{Findings[], Failed[], (NotApplicable[])}
        │
        ▼  per finding
   Normalizer   ─(FR-N1, P1)─▶  domain.Finding (CWE-keyed, validated)
        ▼
   Deduplicator ─(FR-N2, P2)─▶  collapsed set (tool-scoped identity)
        ▼
   SeverityMapper ─(FR-N3, P3)▶  valid Severity (unknown→Info)
        ▼
   FindingsStore  ─(FR-N7)────▶  persisted (bounded, encrypted)
        │
        │   ── at scan completion ──
        ▼
   CoverageAccountant ─(FR-N5, P4/P6)─▶ domain.Coverage (over spec.Items, ownership-derived)
        ▼
   ComplianceMapper ─(FR-N4, P5/P7)──▶ FrameworkLabels (projection, report-only)
        ▼
   ReportBuilder ─(FR-N6)────────────▶ Report{Findings(CWE), Coverage(PSID), rev, labels}
```

Two axes never merge: **Coverage** comes from ownership; **FrameworkLabels** from projection. The mapping table feeds only the latter (P6).

---

## 2. The coverage model, worked (FQ1/FQ1b)

**Boot (once):**
- `CWEOwnership`: `observation:CWE-615 → {CWE-615}`, `humble → {CWE-319,614,1004,1275,1021}`, `subzy → {no-canonical-cwe}`, …
- Responsibility sets: `Project(ownedCWEs, KISA)` ∪ attacker-axis owners. e.g. humble → {PS-001,015,016,017,024} (weakness axis); subzy → {PS-025} (via `AttackTechniqueTable`, since no-canonical-cwe projects to ∅ on the weakness axis).
- Completeness check: `⋃ responsibility ∪ delegatedItems` must equal `spec.Items` (FR-N5c; item set from config). Boot fails otherwise.

**Per scan:**
- PS-009: owner = observation CWE-615/540/200 checkers. They ran ok → `StatusChecked`. Any issue lands as a `Finding{CWEs:[CWE-615],...}`; the report labels it KISA=PS-009 via projection.
- PS-025: attacker-axis item — owner = subzy (via `AttackTechniqueTable`). subzy ran ok → `StatusChecked`; if subzy failed → `StatusToolFailed`. No CWE mapping consulted; the report labels it ATT&CK T1584.001.
- PS-014: delegated → `StatusDelegated{ASM}`. PS-005/006/026 → `StatusDelegated{Nuclei}`.
- A cookie item on a cookie-less target: humble signals N/A → `StatusNotApplicable` (FQ5).
- `Coverage.Validate(spec)` confirms the partition over `spec.Items` (P4).

**P6 in action:** add an `OWASP:` section to the mapping file. `FrameworkLabels` gains OWASP labels; the boot responsibility sets (KISA slice) and every scan's `Coverage` are **byte-identical**. Test asserts this.

---

## 3. PBT realization (P1–P7 — this unit owns them)

| # | Generator → assertion |
|---|---|
| P1 | random/malformed tool output → `Normalize` never panics; known fixture → known Finding |
| P2 | random finding list → `dedup∘dedup == dedup` and permutation-invariant (set eq) |
| P3 | random (raw,tool) severity strings → `Map` ∈ {Info..Critical} always |
| P4 | random scan outcomes → `Account` yields a valid partition over `spec.Items` (`Coverage.Validate(spec)` nil) |
| P5 | every owned CWE → `Project(cwe,KISA)` non-missing (≥1 item or explicit empty) |
| P6 | snapshot Coverage; add OWASP mapping entries; `Account` again → Coverage unchanged |
| P7 | random finding+framework → every projected item traces back to a CWE in the finding |

Generators + assertions are implemented at findings **Code Generation** (PBT FULL). The property statements are frozen here.

---

## 4. Boundaries & handoff
- **Consumes**: `domain.Finding`, `domain.WorkItemOutcome`, `domain.Coverage`, the mapping/severity/ownership configs.
- **Produces**: persisted findings + `Report`.
- **Does not** fetch, schedule, or run checkers — those are scan/platform.
- **Domain addition pending** (FQ5): `WorkItemOutcome.NotApplicable []CheckerID` — added when the scan unit (the producer) is built; until then a ran checker ⇒ `StatusChecked`.
- Next unit (order Q3=A): **scan** — implements the checkers that own the CWEs this unit accounts.
