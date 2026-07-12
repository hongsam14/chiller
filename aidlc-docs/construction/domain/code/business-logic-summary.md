# Code Summary — Unit `domain` (U0)

**Stage**: 🟢 CONSTRUCTION → `domain` → Code Generation · **Date**: 2026-07-08
**Module**: `github.com/enki-offen/chiller` (go 1.23) · **Verified**: `go vet` clean, `go test ./internal/domain/...` pass, `make arch-check` OK (on go 1.26).

---

## Files created (application code — workspace root)

### Project scaffold (greenfield, one-time)
- `go.mod` — module `github.com/enki-offen/chiller`, go 1.23
- `Makefile` — `build` / `test` / `lint` / `tidy` / **`arch-check`** (NFR-02 import guard, works without golangci-lint)
- `.golangci.yml` — **depguard** rules enforcing NFR-02 (data plane ⊅ platform; domain imports no unit)
- `.gitignore` — Go additions
- Directory skeleton: `cmd/chiller/`, `internal/{domain,platform,scan,findings}/`, `configs/mappings/`, `deploy/`, `test/` (`.keep` placeholders where no code yet)

### `internal/domain/` — the shared kernel
| File | Types | Helpers |
|---|---|---|
| `ids.go` | `ScopeKind`(URL/Host), `CheckerID`, `PSID`, `Framework`, `DelegateTarget`, `ScanID`, `WorkItemID`, `Timestamp` | — |
| `severity.go` | `Severity` (Info<Low<Medium<High<Critical) | `String`, `Valid`, `Max` (R3/R4) |
| `cwe.go` | `CWE`, `NoCanonicalCWE` sentinel | `numericID` (ordering) |
| `finding.go` | `Finding`, `Evidence` | `NormalizeCWEs` (R1), `IdentityKey` (R2) |
| `coverage.go` | `ItemStatus`(4-state), `ItemRecord`, `Coverage`, `CoverageSpec` | `Validate(spec)` (R5/R6, config-driven) |
| `checker.go` | `Checker` iface, `ScanContext`, `Target` | — |
| `har.go` | `HAREntry`, `Header`, `Cookie` | — |
| `workitem.go` | `WorkItem`, `WorkItemOutcome` | — |

### `internal/domain/*_test.go`
- `severity_test.go` — ordering, `Valid`, `Max` (commutativity)
- `finding_test.go` — `NormalizeCWEs` sort/dedup, **idempotence**, sentinel exclusivity, no-input-mutation; `IdentityKey` stable-under-reorder + tool/URL distinction (Q1=B)
- `coverage_test.go` — `Validate(spec)` accepts a full partition incl. delegations; rejects missing/extra item, delegated-without-target, target-without-delegation, non-delegable-delegated, wrong-target, invalid status; **+ a custom non-KISA spec** proving kernel independence

---

## Type ↔ entity mapping
Every type in `domain-entities.md` is realized 1:1. Two-axis separation is structural: `Finding.CWEs` (expression) vs `Coverage`/`PSID` (accounting); no PS-ID field on `Finding`.

## Code review fixes applied (2026-07-08)
- **M1** `Finding.Validate()` added — enforces ≥1 CWE / sentinel-exclusivity / valid Severity / non-empty TargetURL+SourceTool (counterpart to `Coverage.Validate`).
- **M2** `Coverage.Validate` now enforces R6's **fixed** delegation map (`delegableItems`): only PS-014→ASM and PS-005/006/026→Nuclei; any other delegated item or wrong target is rejected.
- **L1** package doc moved to `doc.go`. **L2** `Cookie` split into explicit `Secure`/`HttpOnly`/`SameSite` + `Attrs`. **L3** non-numeric-CWE sort fallback tested. **L4** `IdentityKey` uses a single unit-separator for all components.
- Re-verified: `go vet` clean, `gofmt` clean, all tests pass, `arch-check` OK.
- **scan-FD contract addition (2026-07-08, FQ2)** `Checker.Check` now returns `CheckResult{Findings, NotApplicable []CWE}`; `WorkItemOutcome` gained `NotApplicable map[CheckerID][]CWE`. N/A is a first-class result (not error/finding). Module re-verified green.
- **Overlap fix → superseded by B (2026-07-08)** `DelegationTarget` (and `AllPSIDs`, `delegableItems`) **removed from the kernel**. Rationale: KISA's item set + delegations are customer-changeable, and `domain` is the shared, stable kernel — so they must not live here. They are now supplied per scan via `CoverageSpec` (from config). `Coverage.Validate(spec)` is generic; the kernel no longer knows KISA's 26 items. `DelegateTarget` + `ASM`/`Nuclei` constants remain (fixed pipeline stages).

## Helper ↔ rule mapping
- `NormalizeCWEs` → **R1** (dedup+sort, sentinel exclusivity)
- `Finding.IdentityKey` → **R2** (tool-scoped dedup key)
- `Severity.Max` → **R3** (same-tool collapse severity)
- `Severity.Valid` → **R4** precondition (mapping totality is enforced in findings; domain guarantees the target scale is well-formed)
- `Coverage.Validate(spec)` → **R5** (totality over `spec.Items` + disjoint partition) + **R6** (delegated⇒`spec.Delegations` target) — config-driven, framework-agnostic

## Deferred to the `findings` unit (not in domain)
Dedup algorithm, severity mapping table (unknown→Info), CWE→framework projection, coverage accounting logic — domain holds only the *types* + trivially-pure invariant helpers. PBT properties **P1–P7** (business-logic-model §3) are implemented there.

## NFR-02 scaffolded
`arch-check` (Makefile) + `.golangci.yml` depguard mechanically forbid data-plane→platform and domain→unit imports. Verified green now (only `domain` has code). Re-runs on every future unit.

## Stories
None implemented directly (domain is the contract). It unblocks US-014/015/016/017/018/020/021 (findings), US-006/007/008-013/030-033 (scan), US-024-028 (platform).
