# Application Design — chiller (consolidated)

**Stage**: INCEPTION → Application Design (artifact 5/5, consolidation) · **Date**: 2026-07-08
**Consolidates**: `components.md` · `component-methods.md` · `services.md` · `component-dependency.md`
**Basis**: requirements r3 · stories · application-design-plan §H + clarifications (Q1-7, AC0-7, BC1-5, DC1-2, EC1-4).

---

## 1. Architecture at a glance

chiller is a **single Go image**, run as `--role=manager|worker` (A-1). Three bounded contexts map to top-level packages with a strict one-way dependency:

```
platform/  (control plane)  ──drives──▶  scan/ + findings/  (data plane)
   ScanAPI, ScanOrchestrator,               Ingestor, ApprovalGate, RateLimiter,
   TaskStore, WorkQueue,                     Fetcher, ResponseCache, Checker seam
   ProgressReporter                          (ObservationEngine + 7 ProbeAdapters),
                                             Normalizer…ReportBuilder, FindingsStore
```

Design pillars (all from approved requirements/stories, realized here):
1. **Control/data plane split** with unidirectional dependency — the checker plugin seam **NFR-02**.
2. **Interface-forced construction + DI (NFR-01)** — every component is an interface with a `New…()` taking interface deps; `main` is the only composition root.
3. **Two checker families behind one `Checker` seam (r3)** — Observation (in-process, 10) + Probe (CLI, 7 tools/12 items); 4 delegated.
4. **CWE = expression axis, PS-ID = accounting axis** — kept apart end to end (FR-14).
5. **Cross-family single-fetch via transient ResponseCache (r3, NFR-16)** — HAR entries, encrypted, wiped.
6. **Cloud-agnostic backends (NFR-04)** — Mongo/Valkey behind repository/queue interfaces.

---

## 2. Component inventory (summary)

**Platform (5)**: `ScanAPI`, `ScanOrchestrator`, `TaskStore`, `WorkQueue`, `ProgressReporter`.
**Scan (data plane)**: `URLIngestor`, `ApprovalGate`, `RateLimiter`, `Fetcher`, `ResponseCache`, `CheckerRegistry` + `Checker` seam → `ObservationEngine` (10 signatures) + `ProbeAdapter`×7 (humble, httpx, testssl, dirhunt, changeme, Corsy, subzy).
**Findings (data plane)**: `Normalizer`, `Deduplicator`, `SeverityMapper`, `ComplianceMapper`, `CoverageAccountant`, `ReportBuilder`, `FindingsStore`.

Full responsibilities → `components.md`; signatures → `component-methods.md`.

---

## 3. Key decisions traced to clarifications

| Decision | Source | Realized in |
|---|---|---|
| Single image, role-selectable | A-1 / Q1=A | services §1, dependency §6 |
| WorkQueue interface + Valkey | Q3=A | `WorkQueue`, dependency §3 |
| One `Checker` + `ScanContext` | Q4=A | components §2.6, methods |
| Declarative mapping file | Q5=A | `ComplianceMapper` |
| Single store + repository iface | Q6=A | `TaskStore`/`FindingsStore` |
| Platform `ScanAPI` | Q7=A | `ScanAPI` |
| Task ≠ WorkItem (2 levels) | AC0 | services §3.2, methods (TaskStore ledger) |
| ZAP/Nuclei removed; 2 families + 4 delegated | Q2/AC1/AC6/BC1/DC1-2 | components §2.6, §3.6 |
| WorkItem scope model | AC0/C-6 | services §3.2 |
| ResponseCache (Valkey, HAR) | EC1-4 / NFR-16 | `ResponseCache`, dependency §5 |
| Cache = transient, not findings | EC2 / FR-12 | dependency §5 |
| Cache only for HTML-fetching URL-scoped | EC3 / C-7 | components §2.5, services §3.2 |
| Checks in-process-parallel, WorkItem stays URL-scoped | EC4 | services §3.2 |

---

## 4. End-to-end flow (condensed)

`SubmitScan` → validate (URLIngestor) → FSM created→running (ScanOrchestrator/TaskStore) → fan out URL-scoped & host-scoped WorkItems (WorkQueue) → per WorkItem: fetch-once→ResponseCache, run checkers (Observation+probes) emitting CWE findings → Normalize→Dedup→Severity→Compliance→persist (FindingsStore) → MarkWorkItemDone → on completion: CoverageAccountant (26 PS-ID, 4-state, delegate targets) → ReportBuilder (both axes + mapping rev) → WipeScan → `GetReport`. Full diagram: `services.md` §2.

---

## 5. How this design satisfies the cross-cutting constraints

- **NFR-02 (seam)**: dependency matrix shows zero data→control edges; new checkers register without orchestrator change. Enforceable import lint (dependency §2).
- **FR-02 + P6 load (NFR-13)**: ResponseCache gives one GET per URL across families.
- **FR-12 / SECURITY-01/03**: transient encrypted cache ≠ persisted findings; findings store holds bounded evidence only; redaction on logs (dependency §5).
- **FR-13 invariant**: accounting isolated in `CoverageAccountant`; projection cannot move it (dependency §4, PBT US-018).
- **SECURITY-11 (isolation)**: `ApprovalGate`/`RateLimiter` in the dedicated `Safety` subdomain module.
- **Resiliency (US-025/027)**: TaskStore CAS ledger for resume; WorkItem-level failure isolation.
- **Cloud-agnostic (NFR-04)**: all backends behind interfaces; no AWS-only service.

---

## 6. Deferred to later stages (not decided here)

- **Functional Design (per unit)**: FSM transition table; ObservationSignature anchors (per item); dedup/severity algorithms; concrete `CWE → PS-ID` mapping table contents (C-5) incl. PS-025 `no-canonical-cwe` (A-3); per-tool output parsers; per-unit PBT property tables.
- **NFR Requirements/Design**: throughput bound (A-4/NFR-14); Warm-Standby vs single-region and blue/green vs canary reconciliation (A-2); ResponseCache sizing/TTL numbers.
- **Infrastructure Design**: Valkey/Mongo topology (shared vs separate instances); secrets (SECURITY-12); least-privilege (SECURITY-06); alerting/audit (SECURITY-14); tool image pinning (SECURITY-10, C-3).

---

## 7. Units preview (input to Units Generation)

Candidate units align to the 3 contexts (authoritative split at Units Generation):
- **Unit `platform`** — orchestration, FSM, queue, API, progress.
- **Unit `scan`** — ingestion, safety, fetch/cache, checker seam + families.
- **Unit `findings`** — normalization, mapping, accounting, reporting, store.

The `Checker` seam and `Finding`/`ScanContext` domain types are the inter-unit contracts.
