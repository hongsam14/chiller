# Code Generation Plan — Unit `findings` (U3)

**Stage**: 🟢 CONSTRUCTION → `findings` → Code Generation (Part 1: Planning) · **Date**: 2026-07-08
**Single source of truth for findings code generation.**

**Unit context**
- Unit: U3 data plane (`internal/findings`) — Normalizer / Deduplicator / SeverityMapper / ComplianceMapper / CoverageAccountant / ReportBuilder / FindingsStore. Owns PBT **P1–P7**.
- Stories: US-014, 015, 016, 017, 018, 019, 020, 021, 022.
- Depends on: `internal/domain` only (NFR-02; enforced by arch-check).
- Consumers: platform (final assembly), scan (emits findings this unit consumes) — later units.
- Spec: `../findings/functional-design/{domain-entities,business-rules,business-logic-model}.md` (FR-N1..N7, FQ1/1b/2/3/4=B/5/6).

**Code location**: workspace root, Go layout. App code → `internal/findings/**`, configs → `configs/`. Docs → `aidlc-docs/construction/findings/code/`.

**Reuse discipline (no overlap with domain — verified 2026-07-08)**: findings **consumes** domain helpers and never reimplements them — `NormalizeCWEs`, `Finding.IdentityKey`, `Finding.Validate`, `Severity.Max`/`Valid`, `Coverage.Validate(spec)`. The KISA item set + delegations are **config** (loaded into `domain.CoverageSpec`) — the kernel no longer holds `AllPSIDs`/`delegableItems`/`DelegationTarget` (B refactor). Dedup/collapse builds on domain primitives; it does not derive its own identity/severity logic.

**External dep**: `gopkg.in/yaml.v3` (mapping + severity config parsing, FQ3/FQ4=B) — widely-used, cloud-agnostic; add to go.mod.

---

## Steps

### Step 1 — Config files + loader types `[ ]` (FQ3, FQ4=B)
- [ ] `configs/mappings/kisa.yaml` — CWE→KISA map + `attackTechnique` section (PS-025→subzy/T1584.001). Indicative rows (authoritative pinned per C-5).
- [ ] `configs/severity.yaml` — per-tool severity tables + `default: SevInfo`.
- [ ] `configs/coverage/kisa.yaml` — `items:` (the authoritative PS set) + `delegations:` → loaded into `domain.CoverageSpec` (B: item set is config, not kernel).
- [ ] `internal/findings/config/` — `MappingTable`, `AttackTechniqueTable`, `SeverityConfig` types + YAML loaders. `Project(cwes, framework)`, `Revision()`.

### Step 2 — SeverityMapper `[ ]` (US-016, P3, FR-N3)
- [ ] `internal/findings/severity/` — `SeverityMapper` iface + impl reading `SeverityConfig`; `Map(raw, tool) → domain.Severity`; unmapped → `SevInfo` (R4).

### Step 3 — Normalizer `[ ]` (US-014, P1, FR-N1)
- [ ] `internal/findings/normalize/` — `Normalizer` iface; per-source parse → `domain.Finding` (CWE-keyed, validated via `domain.Finding.Validate`); total (no panic on malformed). Per-tool parsers are stubs/table-driven (real tool-output parsing detailed with scan adapters; here: the normalization contract + a generic path).

### Step 4 — Deduplicator `[ ]` (US-015, P2, FR-N2)
- [ ] `internal/findings/dedup/` — `Deduplicator` iface; group by `domain.Finding.IdentityKey()`; collapse (max severity via `domain.Max`, evidence union bounded). Idempotent + order-independent.

### Step 5 — ComplianceMapper `[ ]` (US-017/018, P5/P7, FR-N4)
- [ ] `internal/findings/compliance/` — `ComplianceMapper` iface over `MappingTable`; `Project(cwes, fw)`; totality (≥1 item or explicit empty); round-trip soundness. Report-labeling only (never coverage).

### Step 6 — CoverageAccountant `[ ]` (US-020/021, P4/P6, FR-N5) — load-bearing
- [ ] `internal/findings/coverage/` — `CoverageAccountant` iface; loads `domain.CoverageSpec` (items + delegations) from config; responsibility sets = `Project(ownedCWEs,KISA) ∪ AttackTechniqueTable owners`; **boot completeness check** (`spec.Items` = weakness ∪ attacker ∪ delegated, partitioned; else error); per-scan status (StatusChecked/NotApplicable/Delegated/ToolFailed) → `domain.Coverage`; `Validate(spec)` must pass. `CWEOwnership` + `CoverageSpec` are inputs (from config/wiring; tests use fixtures).
- [ ] **Delegations from `spec.Delegations`** (config, `domain.CoverageSpec`) — the kernel no longer holds the map (B refactor). Delegated items = keys of `spec.Delegations`; target = its value.

### Step 7 — ReportBuilder + FindingsStore `[ ]` (US-019/022, FR-N6/N7)
- [ ] `internal/findings/report/` — `ReportBuilder` iface; assemble `Report{Findings, Coverage, MappingRevision, FrameworkLabels}`.
- [ ] `internal/findings/store/` — `FindingsStore` iface + **in-memory impl** (for tests/local); bounded-evidence + redaction helper. Mongo impl deferred to Infrastructure/Build.

### Step 8 — Unit + property tests (PBT FULL) `[ ]`
- [ ] `*_test.go` per component. **P1–P7** as property tests (`testing/quick` or table+generated inputs): P1 normalize-total, P2 dedup idempotent/commutative, P3 severity-total, P4 coverage-partition, P5 projection-totality, P6 projection-⇏-coverage, P7 mapping round-trip.

### Step 9 — Code summary `[ ]`
- [ ] `aidlc-docs/construction/findings/code/business-logic-summary.md` — files, component↔rule↔property map, deferred items (Mongo store, per-tool parsers, WorkItemOutcome.NotApplicable at scan unit).

---

## Story traceability
US-014→Normalizer, US-015→Deduplicator, US-016→SeverityMapper, US-017/018→ComplianceMapper, US-019→ReportBuilder, US-020/021→CoverageAccountant, US-022→FindingsStore. Marked [x] as each step completes.

## Verification (run after generation)
`go vet ./internal/findings/...` · `go test ./internal/findings/...` · `make arch-check` (findings must not import platform).

## Scope note
No API/deploy artifacts here (findings is a library unit; API=platform, deploy=infra). Mongo store impl + real per-tool parsers deferred (documented). PBT is FULL — property tests are mandatory this unit.

## Checkboxes
- [x] Step 1 · [x] Step 2 · [x] Step 3 · [x] Step 4 · [x] Step 5 · [x] Step 6 · [x] Step 7 · [x] Step 8 · [x] Step 9 — **verified: build/vet/test/gofmt/arch-check all green**
