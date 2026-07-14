# Unit Dependency Matrix — chiller

**Stage**: INCEPTION → Units Generation (artifact 2/3) · **Date**: 2026-07-08
**Basis**: `unit-of-work.md` · `component-dependency.md` (NFR-02 one-way rule)

---

## 1. Dependency matrix (row imports column)

| ↓ imports → | domain (U0) | findings (U3) | scan (U2) | platform (U1) |
|---|---|---|---|---|
| **domain (U0)** | – | ✗ | ✗ | ✗ |
| **findings (U3)** | ✔ | – | ✗ | ✗ |
| **scan (U2)** | ✔ | ✗ | – | ✗ |
| **platform (U1)** | ✔ | ✔ (iface) | ✔ (iface) | – |

- **domain** imports nothing → no cycles possible.
- **findings** and **scan** import only **domain** → they are peers, mutually independent (can be built/tested in isolation).
- **platform** imports **domain** + the data-plane **interfaces** it orchestrates (injected at `cmd/`), never their internals.
- **No column ever points back up**: nothing imports platform (NFR-02 satisfied at the unit level).

```
        platform (U1)
        ╱          ╲   (depends on interfaces, one-way)
   scan (U2)     findings (U3)
        ╲          ╱
         domain (U0)      ← imported by all, imports none
```

---

## 2. Inter-unit contracts (the only coupling points)

| Contract (in U0 `domain`) | Producer | Consumer |
|---|---|---|
| `Checker` (iface) + `ScanContext` | scan (implements) | platform (invokes via registry) |
| `Finding` (CWE-keyed) | scan (emits) | findings (normalizes/maps/accounts) |
| `Coverage`, `Report` | findings (produces) | platform/ScanAPI (serves) |
| `WorkItem`, `WorkItemOutcome` | platform (defines/schedules) | scan+findings (execute, report outcome) |

All four live in `domain`; changing one is a deliberate contract change reviewed across units.

---

## 3. Build / construction order (dependency topological sort)

`domain → findings → scan → platform` (Q3=A). Valid because the matrix is a DAG:
- domain: 0 deps → first.
- findings, scan: depend only on domain → buildable once domain exists (findings first per Q3, to fix Finding lifecycle/accounting before its producers).
- platform: depends on domain + findings + scan interfaces → last.

Each unit's CONSTRUCTION (Functional Design → Code Gen) completes before the next begins (per CLAUDE.md per-unit loop).

---

## 4. Runtime data-flow across units (not import direction)

Import direction (§1) ≠ runtime call direction. At runtime the control plane drives:

```
platform.ScanOrchestrator
   ├─(schedules WorkItem)→ scan.Fetcher/Checkers ─(emit Finding)→ findings.Pipeline ─(persist)→ FindingsStore
   └─(on completion)────────────────────────────→ findings.CoverageAccountant → findings.ReportBuilder → platform.ScanAPI
```

- Runtime: platform → scan → findings → platform (assembly). 
- Compile-time: everyone → domain; platform → {scan,findings} interfaces. No cycle either way.

---

## 5. Shared infrastructure (cross-unit, injected — not a unit)

Backends are configured in `cmd/` and injected as interfaces; they are *not* units:
- **Mongo** (default) → `TaskStore` (platform), `FindingsStore` (findings) — separate collections, same logical store (Q6=A).
- **Valkey** → `WorkQueue` (platform) + `ResponseCache` (scan) — may share or split instance (Infra Design).
- All behind repository/queue/cache interfaces → cloud-agnostic swap (NFR-04).

---

## 6. Enforcement (carry to Build & Test) — NFR-02 made mechanical, not aspirational

### 6.1 Why import direction *is* the rule
In Go, a package dependency **is** an `import`. If `internal/scan` does not import `internal/platform`, then scan code **cannot** call platform functions or reference platform types — the compiler forbids it. So the abstract rule "the data plane must not depend on the control plane" reduces to two concrete, checkable import constraints:

- **R1** — no file under `internal/scan/**` or `internal/findings/**` imports `internal/platform` (directly *or* transitively).
- **R2** — no file under `internal/domain/**` imports any chiller unit (`internal/platform|scan|findings`) → keeps the kernel neutral and the graph acyclic.

"Add a checker without touching the orchestrator" follows for free: the orchestrator sees only `domain.Checker`, so a new checker registers into `CheckerRegistry` with **zero** platform edits.

### 6.2 Three enforcement levels (pick one; b is the baseline)

**(a) Minimal — `go list` in the Makefile / CI:**
```bash
# fails the build if the data plane pulls in platform (transitive deps included)
go list -deps ./internal/scan/... ./internal/findings/... \
  | grep -q 'enki-offen/chiller/internal/platform' \
  && { echo "NFR-02 violation: data plane imports platform"; exit 1; } || true
# kernel neutrality
go list -deps ./internal/domain/... \
  | grep -Eq 'enki-offen/chiller/internal/(platform|scan|findings)' \
  && { echo "NFR-02 violation: domain imports a unit"; exit 1; } || true
```
`go list -deps` expands the **transitive** graph, so an indirect (multi-hop) violation is still caught.

**(b) Baseline — `golangci-lint` `depguard`** (declarative, runs with the normal lint step):
```yaml
# .golangci.yml
linters-settings:
  depguard:
    rules:
      data-plane:
        files: ["**/internal/scan/**", "**/internal/findings/**"]
        deny:
          - pkg: "github.com/enki-offen/chiller/internal/platform"
            desc: "data plane must not depend on control plane (NFR-02)"
      domain-kernel:
        files: ["**/internal/domain/**"]
        deny:
          - pkg: "github.com/enki-offen/chiller/internal/platform"
          - pkg: "github.com/enki-offen/chiller/internal/scan"
          - pkg: "github.com/enki-offen/chiller/internal/findings"
            desc: "domain kernel must import no unit (no cycles)"
```

**(c) Strongest — architecture test** (`golang.org/x/tools/go/packages` loads the import graph; a plain `go test` asserts the forbidden edges are absent), or a declared allowed-edge spec via `go-arch-lint`. Runs as a normal test → red on violation.

### 6.3 What a violation looks like
A dev adds `import ".../internal/platform"` inside `internal/scan/checking/observation.go` (e.g. to reuse a helper). CI lint step fails:
```
internal/scan/checking/observation.go:7: import 'github.com/enki-offen/chiller/internal/platform'
  is not allowed from list 'data-plane' (NFR-02)
```
→ PR blocked. Even if a human reviewer misses it, the pipeline does not.

### 6.4 Import direction ≠ runtime call direction (why this is still a DAG)
- **Compile-time (import)**: one-way — everyone → `domain`; `platform` → `{scan,findings}` interfaces. No upward import.
- **Runtime (call)**: `platform` drives `scan` → `findings` → back to `platform` (report assembly). This "upward" flow goes through **injected interfaces** (wired in `cmd/`), so `scan`/`findings` never `import` `platform`. The import graph stays acyclic while the runtime flow completes the loop.
