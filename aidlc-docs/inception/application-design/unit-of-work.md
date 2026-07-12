# Units of Work — chiller

**Stage**: INCEPTION → Units Generation (artifact 1/3) · **Date**: 2026-07-08
**Basis**: `../../plans/unit-of-work-plan.md` (approved; Q1-5=A) · requirements r4 · stories · `components.md`/`services.md`/`component-dependency.md`

**Deployment reality (A-1)**: chiller is **one deployable image**, run as `--role=manager|worker`. The units below are **logical modules (Go packages) within that one binary**, not independently deployable services. Role split (manager/worker) is orthogonal to the module split (see §4).

---

## 1. Units

### U0 — `domain` (shared kernel) · `internal/domain`
- **Purpose**: the inter-unit contract. Types every other unit shares, so no unit needs to know another unit's internals.
- **Contents**: `Finding` (CWE-keyed), `ScanContext`, `Checker` interface, `CheckerID`, `ScopeKind`, `Severity`, `PSID`, `Framework`, `Coverage`, `ItemStatus`, `DelegateTarget`, `HAREntry`, `NoCanonicalCWE` sentinel.
- **Depends on**: nothing (imports no other unit — guarantees no cycles).
- **Stories**: none directly; it is the contract the story-bearing units implement against.
- **Owner concern**: changes here ripple everywhere → smallest, most stable, most reviewed unit.

### U1 — `platform` (control plane) · `internal/platform`
- **Purpose**: drive scans; the control plane. *When/how*, never *what*.
- **Components**: `ScanAPI`, `ScanOrchestrator`, `TaskStore`, `WorkQueue`, `ProgressReporter`.
- **Responsibilities**: scan FSM (US-024), fan-out into WorkItems (US-026), WorkItem-level failure isolation (US-027), CAS resume (US-025), versioned status/report API + authz (US-001/023), observability (US-028), deploy/rollback hooks (US-029, mostly infra).
- **Depends on**: `domain`; and (by interface, injected) the data-plane entry points. **Never** imported by scan/findings (NFR-02).
- **Role**: primarily `manager`; also hosts the worker's WorkItem consumer loop.

### U2 — `scan` (data plane) · `internal/scan`
- **Purpose**: acquire and inspect — turn URLs into CWE-keyed findings by observation.
- **Sub-modules**:
  - `ingestion/` — `URLIngestor` (US-003)
  - `safety/` — `ApprovalGate`, `RateLimiter` (US-004/005; isolated per SECURITY-11)
  - `fetch/` — `Fetcher`, `ResponseCache` (US-002; `<script src>` shallow discovery C-8)
  - `checking/` — `CheckerRegistry`, `ObservationEngine` + 10 signatures (US-006, 008~013, 030~033), `ProbeAdapter`×7 (US-007)
- **Depends on**: `domain` only.
- **Role**: `worker`.

### U3 — `findings` (data plane) · `internal/findings`
- **Purpose**: normalize, map, account, report — the two-axis engine.
- **Components**: `Normalizer`, `Deduplicator`, `SeverityMapper`, `ComplianceMapper` (FR-14 mapping layer), `CoverageAccountant`, `ReportBuilder`, `FindingsStore`.
- **Responsibilities**: CWE-keyed normalization (US-014), dedup (US-015), severity (US-016), CWE→framework projection (US-017/018), 26-PS-ID coverage + delegation targets (US-020/021), report assembly (US-019), encrypted bounded-evidence persistence (US-022).
- **Depends on**: `domain` only.
- **Role**: `worker` (per-WorkItem pipeline) + `manager` (final accounting/report assembly).
- **Note**: this unit hosts most of the §7 PBT properties.

---

## 2. Code organization strategy (greenfield, Q4=A)

```
chiller/
├── cmd/chiller/            # main; parses --role=manager|worker; composition root (wiring)
├── internal/
│   ├── domain/             # U0 shared kernel (no intra-project imports)
│   ├── platform/           # U1  api/ orchestrator/ taskstore/ workqueue/ progress/
│   ├── scan/               # U2  ingestion/ safety/ fetch/ checking/{observation,probe}/
│   └── findings/           # U3  normalize/ dedup/ severity/ compliance/ coverage/ report/ store/
├── configs/mappings/       # CWE→{KISA,…} declarative versioned tables (FR-14); kisa.yaml (MVP)
├── deploy/                 # Helm chart / OCI (AWS-free, NFR-04/09)
├── test/                   # PBT generators + integration fixtures (benign/malicious response fixtures)
├── go.mod                  # module github.com/enki-offen/chiller
└── Makefile
```

- **`internal/`** prevents external import of chiller's guts (only `cmd/` wires).
- **DI/interface-forced construction (NFR-01)**: each sub-package exports interfaces + `New…()`; `cmd/chiller` is the only place concretes are instantiated.
- **Import rule (enforceable, NFR-02)**: `scan/` and `findings/` may import `domain/` only; `platform/` may import `domain/` + the data-plane *interfaces*; nothing imports `platform/`. A CI `go list`/lint gate asserts this (see `component-dependency.md` §2).

---

## 3. Construction order (Q3=A, dependency-driven)

Per-unit CONSTRUCTION loop runs in this order:

1. **U0 domain** — fix the contracts first (Finding/Checker/ScanContext). Cheapest, unblocks all.
2. **U3 findings** — defines the Finding lifecycle, mapping, and accounting the others target.
3. **U2 scan** — emits findings via checkers into the (now-defined) pipeline.
4. **U1 platform** — orchestrates the (now-existing) data plane end to end.

Rationale: lower layers (contract → findings semantics → producers → orchestrator) stabilize before their consumers are built, minimizing rework.

---

## 4. Units × roles (orthogonality)

| Unit | in `manager` | in `worker` |
|---|---|---|
| domain | ✔ (types) | ✔ (types) |
| platform | ✔ API+FSM+fan-out+final assembly | ✔ WorkItem consumer loop |
| scan | – | ✔ fetch+checkers |
| findings | ✔ final accounting/report | ✔ per-WorkItem normalize→persist |

One binary contains all 4 units; `--role` selects which entry points `cmd/chiller` wires and starts.
