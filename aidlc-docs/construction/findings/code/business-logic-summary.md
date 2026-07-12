# Code Summary — Unit `findings` (U3)

**Stage**: 🟢 CONSTRUCTION → `findings` → Code Generation · **Date**: 2026-07-08
**Verified**: `go build ./...`, `go vet ./...`, `go test ./...` all pass; `gofmt` clean; `make arch-check` OK; `go mod tidy` (added `gopkg.in/yaml.v3`).

---

## Files (application code — workspace root)

### Config (data)
- `configs/mappings/kisa.yaml` — CWE→KISA map (weakness axis) + `attackTechnique` (PS-025→subzy/T1584.001). Indicative rows; authoritative pinned at Functional Design (C-5).
- `configs/coverage/kisa.yaml` — `items:` (the KISA accounting set) + `delegations:` → `domain.CoverageSpec` (refactor B: item set is config, not kernel).
- `configs/severity.yaml` — per-tool severity tables + `default: Info` (FQ4=B).

### `internal/findings/`
| Package | Type / func | Story · Property |
|---|---|---|
| `config` | `MappingTable` (`Project`, `Revision`, `AttackOwners`, `KnownCWE`), `SeverityConfig` (`Map`), `LoadCoverageSpec`→`domain.CoverageSpec` | FR-14, FQ3/4=B |
| `severity` | `Mapper.Map` (unmapped→Info) | US-016 · P3 |
| `normalize` | `Normalizer` (`Normalize`/`NormalizeAll`) — `RawFinding`→`domain.Finding`, total | US-014 · P1 |
| `dedup` | `Deduplicator.Dedup` — group by `IdentityKey`, collapse max-severity/bounded-evidence | US-015 · P2 |
| `compliance` | `Mapper.Project`/`Revision` — CWE→framework labels (report only) | US-017/018 · P5/P7 |
| `coverage` | `Accountant` — responsibility sets + boot completeness check + `Account`→`domain.Coverage` | US-020/021 · P4/P6 |
| `report` | `Builder.Build` — two-axis `Report{Findings, Coverage, MappingRevision, FrameworkLabels}` | US-019 |
| `store` | `FindingsStore` iface + `InMemory` (copy-on-save) | US-022 |

### Tests (PBT FULL)
- P1 `normalize`: total (500 random incl. garbage, never panics; invalid→drop count)
- P2 `dedup`: idempotent + order-independent set (200 random)
- P3 `severity`/`config`: total (unmapped→Info)
- P4 `coverage`: valid partition over `spec.Items` (300 random outcomes) + delegated/tool-failed/N-A cases
- P5 `compliance`: no-canonical→explicit empty, absent framework→empty
- P6 `coverage`: **adding an OWASP mapping section leaves coverage byte-identical** (the load-bearing invariant)
- P7 `compliance`: projected items trace back to finding CWEs
- boot check: gap / owned-not-in-spec / unmapped-owned-CWE all fail startup

## Coverage model realized (FQ1/FQ1b/FQ6, refactor B)
- Checkers own CWEs (`CWEOwnership`); KISA responsibility = `Project(ownedCWEs, KISA)` ∪ attacker-axis owners.
- **P6 holds structurally**: `Account` reads ownership + KISA slice + delegations only — never the OWASP/CAPEC/ATT&CK label sections. Test proves it.
- No-canonical (PS-025) via the attacker axis (ATT&CK), owner subzy.
- `domain.CoverageSpec` (item set + delegations) comes from config → kernel stays framework-agnostic.

## Reuse (no domain overlap)
Consumes `domain.NormalizeCWEs`, `Finding.IdentityKey`, `Finding.Validate`, `Severity.Max`, `Coverage.Validate(spec)` — no reimplementation. `arch-check` confirms findings imports domain only (never platform).

## Deferred (documented)
- **Mongo `FindingsStore`** impl → Infrastructure/Build (in-memory ships now).
- **Real per-tool parsers** (tool output → `RawFinding`) → scan adapters.
- **`domain.WorkItemOutcome.NotApplicable`** channel → added at scan-unit construction (accountant already accepts N/A input).
- **Authoritative CWE↔PS-ID rows** → pinned at this unit's Functional-Design follow-up (C-5); current rows indicative.
