# Business Rules — Unit `domain` (U0)

**Stage**: 🟢 CONSTRUCTION → `domain` → Functional Design (artifact 2/3) · **Date**: 2026-07-08
**Basis**: Q1=B, Q1b→DQ2=A, Q2=A, Q3=A/Info, Q4=A, Q5=A, DQ1=A · requirements r4 §6/§7

These are contract-level rules every unit must honor. Implementations live in the findings/scan/platform units; the *rules* are fixed here.

---

## R1 — CWE-set normalization (identity precondition)
A `Finding.CWEs` is stored **normalized**: deduplicated, sorted ascending by numeric CWE id, `NoCanonicalCWE` (if present) is the sole element (never mixed with real CWEs). This makes the identity key (R2) stable regardless of emission order.

## R2 — Finding identity & dedup (Q1=B, US-015, FR-06)
Two findings are **the same** iff `(normalized CWEs, TargetURL, SourceTool)` are equal. Dedup is therefore **tool-scoped** — it collapses a tool's own repeats, and does **not** merge across tools.

**FR-06 reconciliation (DQ1=A)**: "no item double-judged across checkers" holds not by cross-tool merging but by **disjoint ownership** — each of the 26 PS-IDs is owned by exactly one checker (r4 §2.1), so two different tools cannot both judge the same item. Cross-tool findings that happen to share a CWE on the same URL are kept distinct (different `SourceTool`), preserving source attribution.

**Dedup properties (PBT, §7)**:
- **Idempotent**: `dedup(dedup(x)) = dedup(x)`.
- **Order-independent (commutative in set)**: `dedup(a ++ b)` ≡ `dedup(b ++ a)` as sets.

## R3 — Same-key collapse rule (DQ2=A)
When a single tool emits ≥2 findings with the **same identity key** (R2), collapse to one:
- **Severity** = `max` over the duplicates (ordinal, R uses `SevInfo<…<SevCritical`).
- **Evidence** = union of snippets, **truncated to the bounded limit** (FR-12) — earliest snippets kept if over limit.
- **SourceTool** unchanged (same by definition of the key).

## R4 — Severity mapping totality (Q3, US-016)
`MapSeverity(rawToolSeverity, sourceTool) → Severity` is a **total function**:
- Every finding ends with a valid `Severity` ∈ {Info,Low,Medium,High,Critical}.
- Any input the per-tool table does not recognize → **`SevInfo`** (Q3b: passive/recon-tier default; do not inflate).
- Property (§7): totality — no input yields an invalid/absent severity.

## R5 — Coverage totality & disjointness (US-020, §7, resiliency)
For any scan, `Coverage` satisfies:
- **Totality**: `dom(Coverage) = { PS-001 … PS-026 }` — all 26 present.
- **Disjoint statuses**: each PS-ID has exactly one `ItemStatus`; the sets `Found / NotApplicable / Delegated / ToolFailed` **partition** the 26 (pairwise disjoint, union = all).
- Property (§7): this partition holds on every scan, including partial-failure scans.

## R6 — Delegation labeling (US-021, FR-11)
- `Status == StatusDelegated` ⟺ `Target ∈ {ASM, Nuclei}` and non-empty.
- Fixed assignment: `PS-014 → ASM`; `PS-005, PS-006, PS-026 → Nuclei`.
- `Delegated` is **not** counted as `Found` or as a gap. chiller never invokes the delegate (pipeline-level, §2.2).

## R7 — tool-failed semantics (Q4, US-027)
- A `Checker.Check` is **all-or-nothing**: it either returns findings (success) or an `error`.
- On `error`, every PS-ID that Check was responsible for becomes `StatusToolFailed` for that scan — never silently `Found`/`NotApplicable`.
- **ObservationEngine exception**: it runs its 10 signatures **independently**; a failing signature marks only *its* PS-ID `tool-failed`, the others judge normally. (So the engine's `Check` aggregates per-signature outcomes rather than being globally all-or-nothing.)

## R8 — CWE → framework projection contract (FR-14, §7)
`Project(cwes, framework) → items`:
- **Totality**: for every CWE a checker can emit, yields ≥1 framework item **or** the explicit `no-canonical-cwe` marker — never silence.
- **N:M**: one finding may map to several items; one CWE may satisfy several PS-IDs. Cardinality never breaks R5 (accounting stays on the PS-ID axis).
- **Projection-does-not-create-coverage (the invariant that protects R5)**: adding a framework's mapping table leaves the PS-ID `Coverage` bit-identical. The CWE (expression) axis cannot move the PS-ID (accounting) axis.
- **N:M round-trip soundness**: every item in `Project(f, F)` maps back to ≥1 CWE in `f.CWEs`; no item is conjured.

## R9 — Evidence bounding & redaction (FR-12, SECURITY-01/03)
- `Evidence.Snippets` are bounded in count and length; full response bodies are **never** part of a `Finding`.
- Sensitive values in evidence are **redacted** before logging (SECURITY-03) and encrypted at rest when persisted (SECURITY-01). `HAREntry.Body` lives only in the transient `ResponseCache`, never in the findings store.

## R10 — Framework scope gating (FR-14, MVP)
- MVP populates only `Framework == "KISA"`. `OWASP`/`CAPEC` tables are Post-MVP; their absence must not fail a scan (projection to an absent framework yields empty, not error).
