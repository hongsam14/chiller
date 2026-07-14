# Findings — Business Rules

**Stage**: 🟢 CONSTRUCTION → `findings` (U3) → Functional Design (artifact 2/3) · **Date**: 2026-07-08
**Basis**: FQ1=A, FQ1b=A, FQ2=A, FQ3=A, FQ4=B, FQ5=A · domain R1–R10 · requirements r5.
Rules the seven findings components implement. domain's R1–R10 are inherited; these are the concrete algorithms.

---

## FR-N1 — Normalization (`Normalizer`, US-014, P1)
- Parse each tool's raw output into `domain.Finding`s, CWE-keyed and framework-agnostic.
- `SourceTool` is set to the checker id (`observation:CWE-615`, `humble`, …); `TargetURL` from the WorkItem.
- Store `CWEs` via `domain.NormalizeCWEs` (R1) and validate via `domain.Finding.Validate` (R1/R4/target/tool).
- **Total** (P1): malformed tool output ⇒ no panic; unparseable → a diagnostic dropped-count, never a crash. A known fixture normalizes to a known `Finding` (oracle).

## FR-N2 — Dedup (`Deduplicator`, US-015, P2)
- Group by `domain.Finding.IdentityKey()` = `(normalized CWEs, TargetURL, SourceTool)` (R2, tool-scoped).
- Collapse a group to one: `Severity = max` (domain.Max, R3), `Evidence` = union of snippets truncated to the bound (R3/R9), `SourceTool` unchanged.
- **Idempotent + order-independent** (P2): `dedup∘dedup = dedup`; result is set-equal regardless of input order.

## FR-N3 — Severity mapping (`SeverityMapper`, US-016, P3)
- Load `SeverityConfig` from file (FQ4=B). `Map(raw, tool)` → per-tool table; **any miss → `SevInfo`** (R4, Q3b).
- **Total** (P3): every finding leaves with a `domain.Severity.Valid()` value.

## FR-N4 — Compliance projection (`ComplianceMapper`, US-017/018, P5/P7)
- `Project(cwes, framework)` = table lookup (§domain-entities 2).
- **Totality** (P5, R8): every CWE a checker can emit yields ≥1 framework item **or** an explicit empty (the `no-canonical-cwe` / empty-list entry) — never a missing key. Boot validation (FR-N5c) asserts every owned CWE has a table entry.
- **Round-trip** (P7): every item in `Project(f, F)` traces back to a CWE in `f.CWEs`.
- Used **only** to build `Report.FrameworkLabels` — never to compute `Coverage` (P6, see FR-N5).

## FR-N5 — Coverage accounting (`CoverageAccountant`, US-020/021, P4/P6)
The load-bearing rules (FQ1/FQ1b). Coverage is on the PS-ID axis, derived from **ownership**, not from projecting finding CWEs.

**(a) Responsibility set.** For each checker, its KISA responsibility =
`Project(ownedCWEs, KISA)` ∪ `{PSID | AttackTechniqueTable[PSID].Owner == checker}`.
This is computed **once at boot** from `CWEOwnership` (weakness axis) + `MappingTable` + `AttackTechniqueTable` (attacker axis, FQ6). So an item is owned either via its CWEs (weakness axis) or via the attacker-axis table.

**(b) Per-scan status** (for each of the 26 PS-IDs):
1. If the PS-ID is in `spec.Delegations` (from config, CoverageSpec) → `StatusDelegated` with that target (R6).
2. Else let `owners` = checkers whose responsibility set includes it.
   - If any `owner` **ran and signaled N/A** for it → `StatusNotApplicable` (FQ5, structural inapplicability only).
   - Else if any `owner` **ran ok** → `StatusChecked`.
   - Else (all owners that were scheduled are in `WorkItemOutcome.Failed`) → `StatusToolFailed` (US-027).
3. The result MUST pass `domain.Coverage.Validate(spec)` (R5 totality over `spec.Items` + disjoint, R6 delegation).

**(c) Boot completeness check** (FQ1b) — fail startup unless:
`⋃(all responsibility sets) ∪ delegatedItems == spec.Items`, and responsibility-covered ∩ delegated == ∅. (`spec.Items` from config — not a hardcoded 26.)
So every PS-ID is reachable via (weakness axis: CWE-mapping) ∪ (attacker axis: AttackTechniqueTable owner) ∪ (delegation) — no silent gap. This is the mechanical guarantee behind R5 totality.

**(d) P6 invariant.** Coverage reads only `CWEOwnership`, `AttackTechniqueTable`(owner field), `MappingTable`(KISA slice), delegations, and per-scan outcomes. **Adding an OWASP/CAPEC/ATT&CK label section changes `FrameworkLabels` but leaves `Coverage` bit-identical** — testable: snapshot Coverage, add OWASP/ATT&CK keys, re-run, assert equal. (The attacker-axis table's `Owner` affects coverage; its `Techniques` labels do not.)

## FR-N6 — Report assembly (`ReportBuilder`, US-019)
- Merge deduped/severity-mapped `Findings` (CWE axis) + `Coverage` (PS-ID axis) + `MappingRevision` + `FrameworkLabels`.
- Neither axis derived at read time; both materialized. `Coverage.Validate(spec)` must pass before a report is emitted.

## FR-N7 — Findings persistence (`FindingsStore`, US-022, FR-12/SEC-01/03)
- Persist `Finding`s with **bounded** `Evidence` only — never full bodies (R9, FR-12).
- Encrypted at rest (SECURITY-01); sensitive evidence redacted before logging (SECURITY-03, R9).
- Repository interface, Mongo default, swappable (NFR-04). Separate collection from Task store (Q6=A).

---

## Property → rule → component map (PBT, §7)
| Property | Rule | Component |
|---|---|---|
| P1 normalization oracle/total | FR-N1 | Normalizer |
| P2 dedup idempotent/commutative | FR-N2 | Deduplicator |
| P3 severity total | FR-N3 | SeverityMapper |
| P4 coverage partition | FR-N5b + Validate | CoverageAccountant |
| P5 projection totality | FR-N4 | ComplianceMapper |
| P6 projection ⇏ coverage | FR-N5d | CoverageAccountant + ComplianceMapper |
| P7 mapping round-trip | FR-N4 | ComplianceMapper |
