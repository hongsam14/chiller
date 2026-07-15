# Component Dependencies — chiller

**Stage**: INCEPTION → Application Design (artifact 4/5) · **Date**: 2026-07-08
**Scope**: dependency matrix, communication patterns, data-flow, and verification of the **unidirectional control→data** rule (NFR-02).

---

## 1. Dependency matrix (row depends on column, via interface)

Contexts: **P**=Platform · **S**=Scan · **F**=Findings.

| ↓ depends on → | ScanAPI | ScanOrch | TaskStore | WorkQueue | ProgReporter | Ingestor | ApprovalGate | RateLimiter | Fetcher | RespCache | Registry/Checker | Normalizer…ReportBuilder | FindingsStore |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| **ScanAPI** (P) | – | ✔ | | | | | | | | | | ✔ (ReportBuilder) | |
| **ScanOrchestrator** (P) | | – | ✔ | ✔ | ✔ | ✔ | | | | ✔ | ✔ | ✔ | |
| **Fetcher** (S) | | | | | | | ✔ | ✔ | – | ✔ | | | |
| **ObservationEngine** (S) | | | | | | | | | | ✔ (read) | – | | |
| **humble/httpx adapter** (S) | | | | | | | | | | ✔ (read) | – | | |
| **testssl/dirhunt/subzy/Corsy/changeme** (S) | | | | | | | ✔ | ✔ | | | – | | |
| **FindingsPipeline** (F) | | | | | | | | | | | | – | ✔ |

**Reading**: ScanOrchestrator (control) depends on many data-plane interfaces (it drives them). **No data-plane row depends on any Platform column.** ✔ only ever points rightward from P into S/F, or within a context — never S/F → P.

---

## 2. The one-way rule — verification (NFR-02)

```
        ┌─────────────── platform/ (control plane) ───────────────┐
        │  ScanAPI → ScanOrchestrator                              │
        │              │ depends-on (iface)                        │
        └──────────────┼──────────────────────────────────────────┘
                       │  ▼ (only direction allowed)
        ┌──────────────┴─── scan/ + findings/ (data plane) ────────┐
        │  Ingestor · ApprovalGate · RateLimiter · Fetcher ·       │
        │  ResponseCache · CheckerRegistry · Observation · Probes · │
        │  Normalizer · Dedup · Severity · Compliance ·            │
        │  CoverageAccountant · ReportBuilder · FindingsStore      │
        │                                                          │
        │  ✗ MUST NOT import platform/                             │
        └──────────────────────────────────────────────────────────┘
```

**Enforceable checks** (carry into Build/Test):
- `scan/` and `findings/` packages have **zero imports** of `platform/`. (A lint/`go list` rule can assert this.)
- `ScanContext` (the only orchestration→checker payload) contains **data only** (target + cached response), no control handles.
- Every constructor takes interfaces; grep for concrete cross-component `&Concrete{}` news outside `main`/wiring = violation.

---

## 3. Communication patterns

| Edge | Pattern | Notes |
|---|---|---|
| P4 ↔ ScanAPI | sync request/response (HTTP), **versioned** | authz deny-by-default (SEC-08) |
| ScanOrchestrator → WorkQueue → worker | async message (WorkItem) | bounded concurrency (US-026); at-least-once + CAS ledger for exactly-once effect |
| Fetcher → ResponseCache | sync put/get (Valkey) | HAR entry; encrypted, TTL, scan-scoped |
| Checker → (returns) → FindingsPipeline | in-process value return | `[]Finding` (CWE-keyed) |
| Components → TaskStore / FindingsStore | sync repository calls | CAS for state (Mongo default) |
| All → ProgressReporter | fire-and-forget (OTel) | metrics/logs/traces |
| chiller ↛ ASM / Nuclei | **none** | delegation is pipeline-level; no runtime call (FR-11, §2.2) |

---

## 4. Data-flow: the two axes stay separated (r2/r3)

```
Checker ──emits──▶ Finding{cwe_ids}          ── EXPRESSION axis (CWE) ──▶ FindingsStore / Report.findings
                        │
                        └─ ComplianceMapper.Project(cwe, KISA) ─┐
                                                                ▼
WorkItem outcomes ──▶ CoverageAccountant ──▶ Coverage{PSID→status,target} ── ACCOUNTING axis (PS-ID) ──▶ Report.coverage
```

- Checkers touch **only** the CWE axis. PS-ID appears **only** inside `CoverageAccountant` / `ComplianceMapper` output.
- **Invariant (PBT, US-018)**: adding an OWASP/CAPEC mapping table changes `Report` projections but leaves `Coverage` (PS-ID accounting) bit-identical — the expression axis cannot move the accounting axis.
- `Report` carries both axes + the `ComplianceMapper.Revision()`.

---

## 5. Transient vs persistent state (FR-12 / NFR-16 boundary)

| Store | Kind | Contents | Lifetime |
|---|---|---|---|
| `ResponseCache` (Valkey) | **transient** | HAR responses (full bodies, bounded) | scan-scoped, wiped post-scan, encrypted |
| `TaskStore` (Mongo) | persistent | Scan aggregate, FSM, WorkItem ledger | retained |
| `FindingsStore` (Mongo) | persistent | Findings — **bounded evidence only, no full bodies** | retained, encrypted |

The dependency graph forbids findings-persistence components from reading ResponseCache after scan end (it is wiped), preventing accidental full-body persistence (FR-12).

---

## 6. Wiring (composition root, per role)

`main` (the only place that news concretes) builds the graph per `--role`:
- **manager**: `NewScanAPI(NewScanOrchestrator(taskStore, workQueue, registry, cache, findingsPipeline, progress), reportBuilder, authN)`.
- **worker**: registers all Checkers into `CheckerRegistry`, wires `Fetcher(approval, rate, cache)`, consumes `WorkQueue`, runs `FindingsPipeline(normalizer, dedup, severity, compliance, findingsStore)`.
Shared infra (TaskStore, WorkQueue, ResponseCache, stores) is configured from env; all cloud-agnostic backends (NFR-04).
