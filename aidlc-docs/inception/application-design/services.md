# Services & Orchestration — chiller

**Stage**: INCEPTION → Application Design (artifact 3/5) · **Date**: 2026-07-08
**Scope**: service definitions, orchestration patterns, the scan pipeline. Control plane sequences; data plane executes.

---

## 1. Services overview

chiller has **one orchestrating service** (`ScanOrchestrator`) and **one edge service** (`ScanAPI`); everything else is a component the orchestrator composes. This keeps *when/how* in one place (control plane) and *what* in the data plane (stories architecture principle, NFR-02).

| Service | Role | Runs in role |
|---|---|---|
| `ScanAPI` | Edge contract: submit / status / report (versioned, authz) | `manager` |
| `ScanOrchestrator` | Control plane: FSM + fan-out + failure isolation + pipeline routing | `manager` (drives) + `worker` (executes WorkItems) |
| `FindingsPipeline` | Composed transform: Normalize→Dedup→Severity→Compliance→Account→Report | `worker` (per WorkItem) + `manager` (final assembly) |

**Role split (A-1)**: one image. `manager` = ScanAPI + orchestration/FSM + final report assembly. `worker` = pulls WorkItems, runs the Checker registry, emits findings. Horizontal scale = more workers.

---

## 2. The scan pipeline (data flow, one scan)

```
                          ┌──────────── manager ────────────┐
  P4 ──SubmitScan──▶ ScanAPI ──▶ ScanOrchestrator.CreateScan/StartScan
                                        │  (FSM: created→running; TaskStore.SaveScan)
                                        │  fan out → WorkItems (C-6 scope)
                                        ▼
                                   WorkQueue ──enqueue──▶ … ──dequeue──▶
                          ┌──────────── worker (×N) ─────────────┐
                          │  per WorkItem:                       │
                          │   URL-scoped:  Fetcher.Fetch(url)     │
                          │     └▶ ApprovalGate→RateLimiter→GET   │
                          │        └▶ ResponseCache.Put (HAR)     │
                          │     ObservationEngine + humble/httpx  │  ← read ResponseCache
                          │     Corsy/changeme (own request)      │
                          │   host-scoped: testssl/dirhunt/subzy  │  ← bypass cache (EC3)
                          │        │ emits []Finding (CWE-keyed)  │
                          │        ▼                              │
                          │   FindingsPipeline (per WorkItem):    │
                          │    Normalize→Dedup→Severity→          │
                          │    Compliance(project)                │
                          │        │ persist: FindingsStore       │
                          │        ▼ MarkWorkItemDone(outcome)    │
                          └──────────────────────────────────────┘
                                        │
                          ┌──────────── manager ────────────┐
                          │  on all WorkItems done/failed:   │
                          │   CoverageAccountant.Account      │  ← 26 PS-ID, 4-state, targets
                          │   ReportBuilder.Build             │  ← findings(CWE)+coverage(PS-ID)+rev
                          │   ResponseCache.WipeScan          │  ← post-scan wipe (FR-12)
                          └──────────────────────────────────┘
                                        │
  P1/P2 ──GetReport──▶ ScanAPI ─────────┘
```

---

## 3. Orchestration patterns

### 3.1 FSM (US-024)
`ScanOrchestrator` owns the Scan FSM; transitions are atomic via `TaskStore.CompareAndSetState`. States (indicative; table pinned at Functional Design): `created → running → {completed | stopped | failed}`, with `running → running` re-entry for resume. No domain logic in transitions — only lifecycle.

### 3.2 Fan-out (US-026, C-6)
On `StartScan`, the orchestrator classifies each checker by `Scope()` and produces WorkItems:
- **URL-scoped WorkItem** = `(one URL × all URL-scoped checkers)` → one `Fetcher.Fetch` populates ResponseCache once; ObservationEngine + humble + httpx read it; Corsy/changeme issue their own scoped requests. Checks run in-process-parallel (EC4).
- **Host-scoped WorkItem** = `(one host × one host-scoped checker)` → testssl/dirhunt/subzy do protocol I/O.
Enqueued to `WorkQueue` with bounded concurrency.

### 3.3 Failure isolation (US-027, resiliency)
A WorkItem that throws is `Nack`'d and recorded as `tool-failed` for its item(s) via `TaskStore.MarkWorkItemDone`. The scan never aborts; other WorkItems proceed. `CoverageAccountant` renders `tool-failed` distinctly (not `found`, not `not-applicable`).

### 3.4 Resume (US-025, CAS)
`ResumeScan` reads `TaskStore.ListUnfinished` (the WorkItem completion ledger = source of truth) and re-enqueues only those. `CompareAndSetState` guarantees exactly one worker owns each item. Because completed URL fetches are in the (possibly still-warm) ResponseCache — or simply re-fetched once on cache miss — resume never double-hits a target beyond one GET per URL (FR-02).

### 3.5 Findings pipeline (composition, US-014→019)
A pure-ish transform chain, composed by DI: `Normalizer → Deduplicator → SeverityMapper → ComplianceMapper → (persist) FindingsStore`, then at scan end `CoverageAccountant → ReportBuilder`. The chain is where the §7 PBT properties live (normalization totality, dedup idempotence, severity totality, projection totality / does-not-create-coverage / N:M soundness).

### 3.6 Delegation (US-021, FR-11)
`CoverageAccountant` marks the 4 delegated items with a target label at accounting time — PS-014→`ASM`, PS-005/006/026→`Nuclei` — from static config. chiller **never invokes** the delegate; delegation is pipeline-level.

---

## 4. Service boundaries & the one-way rule

- `ScanAPI` → `ScanOrchestrator` → data-plane components. Never the reverse.
- Data-plane components (`scan/`, `findings/`) do **not** import `platform/`. The orchestrator injects what they need as interfaces; they return values/findings.
- `ScanContext` is the only thing crossing from orchestration into a `Checker` — it carries data (target + cached response), not control.

This is the checker plugin seam (NFR-02): a new `ProbeAdapter` or `ObservationSignature` is registered into `CheckerRegistry`; the orchestrator, unchanged, fans it out by its declared `Scope()`.
