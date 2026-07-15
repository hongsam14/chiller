# Code Generation Plan — Unit `domain` (U0)

**Stage**: 🟢 CONSTRUCTION → `domain` → Code Generation (Part 1: Planning) · **Date**: 2026-07-08
**Single source of truth for domain code generation.** Execute steps in order; mark [x] as done.

**Unit context**
- Unit: U0 shared kernel — pure contract types + pure helpers. No I/O, no infra.
- Stories: none directly (contract for all). Implements the *type-level* spec from `../domain/functional-design/*` (entities + R1–R10 + P1–P7).
- Dependencies: none (domain imports nothing — R: acyclic kernel).
- Consumers: findings, scan, platform units.
- **Layers N/A** for this unit: API, Repository, Frontend, DB migration — a types-only kernel has none. Only *Business Logic (types+pure helpers)* + *Unit Testing* + *Summary* apply.

**Code location** (from aidlc-state.md; Go standard layout, unit-of-work.md §2 — overrides the generic `src/` pattern):
- Application code → **workspace root** (`/cmd`, `/internal/domain`, `go.mod`, …). NEVER in aidlc-docs/.
- Markdown summary → `aidlc-docs/construction/domain/code/`.

---

## Steps

### Step 1 [x] — Project structure setup (greenfield, one-time) `[ ]`
- [ ] `go.mod` — module `github.com/enki-offen/chiller`, go 1.23
- [ ] Directory skeleton: `/cmd/chiller/`, `/internal/domain/`, `/internal/platform/`, `/internal/scan/`, `/internal/findings/`, `/configs/mappings/`, `/deploy/`, `/test/` (empty `.keep` where no code yet)
- [ ] `Makefile` — `build`, `test`, `lint`, and `arch-check` (NFR-02 import guard) targets
- [ ] `.golangci.yml` — **depguard** rules enforcing NFR-02 (data plane ⊅ platform; domain imports no unit) per `unit-of-work-dependency.md` §6.2
- [ ] `.gitignore` (Go)
- **Note**: only `internal/domain` gets real code this unit; other dirs are placeholders for later units.

### Step 2 [x] — Domain types generation `[ ]` (US: contract for all)
Target: `/internal/domain/`. One file per cohesive group (per `domain-entities.md`):
- [ ] `ids.go` — `CheckerID`, `ScopeKind`(+consts), `PSID`, `Framework`, `DelegateTarget`, `ScanID`, `WorkItemID`, `Timestamp`
- [ ] `severity.go` — `Severity` + consts (Info<Low<Medium<High<Critical)
- [ ] `cwe.go` — `CWE`, `NoCanonicalCWE` sentinel
- [ ] `finding.go` — `Finding`, `Evidence`
- [x] `coverage.go` — `ItemStatus`(+consts), `ItemRecord`, `Coverage`, `CoverageSpec` *(B refactor: `AllPSIDs` removed — item set is config)*
- [ ] `checker.go` — `Checker` iface, `ScanContext`, `Target`
- [ ] `har.go` — `HAREntry`, `Header`, `Cookie`
- [ ] `workitem.go` — `WorkItem`, `WorkItemOutcome`

### Step 3 [x] — Pure domain helpers `[ ]` (R1–R5 that are pure/contract-level)
- [ ] `finding.go`: `NormalizeCWEs([]CWE) []CWE` (R1 — dedup+sort, NoCanonical exclusivity); `(Finding) IdentityKey() string` (R2 — `(normalizedCWEs,TargetURL,SourceTool)`)
- [ ] `severity.go`: `Max(a,b Severity) Severity` (R3)
- [x] `coverage.go`: `(Coverage) Validate(spec CoverageSpec) error` (R5 — totality over `spec.Items` + 4-state disjoint; Delegated⇒`spec.Delegations` target, R6) *(B: config-driven)*
- **Excluded** (belong to findings unit): dedup algorithm, severity table, mapping projection — domain only holds the *types* + trivially-pure invariant helpers.

### Step 4 [x] — Domain unit tests `[ ]`
Target: `/internal/domain/*_test.go`.
- [ ] `severity_test.go` — total ordering + `Max` table tests
- [ ] `finding_test.go` — `NormalizeCWEs` idempotence (`normalize∘normalize=normalize`) + NoCanonical exclusivity; `IdentityKey` stability under CWE reorder (property-style)
- [ ] `coverage_test.go` — `Validate` accepts a full 26-partition; rejects missing PS-ID, overlapping/duplicate status, Delegated-without-target (R5/R6)
- **Note**: P1–P7 full property tests live in the *findings* unit (they concern dedup/severity-mapping/projection there); domain tests cover only the kernel-owned helpers.

### Step 5 [x] — Business logic summary (markdown) `[ ]`
- [ ] `aidlc-docs/construction/domain/code/business-logic-summary.md` — files created, type↔entity mapping, helper↔rule mapping, what's deferred to findings, how NFR-02 is scaffolded (.golangci.yml).

---

## Story traceability
domain implements no story directly; it provides the contract enabling US-014/015/016/017/018/020/021 (findings) and US-006/007/008-013/030-033 (scan) and US-024-028 (platform). Verified against `domain-entities.md` §8 + `business-logic-model.md` §3 (P1–P7 owners).

## Scope / step count
5 steps. Pure-kernel unit: no API/Repo/Frontend/DB/deploy-artifact steps (N/A). Deployment artifacts (Helm/Dockerfile) are generated with the *platform* unit (last), not here.

## Checkboxes (progress)
- [x] Step 1 · [x] Step 2 · [x] Step 3 · [x] Step 4 · [x] Step 5 — **verified: go vet clean, tests pass, arch-check OK**
