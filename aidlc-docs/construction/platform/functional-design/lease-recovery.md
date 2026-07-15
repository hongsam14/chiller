# Functional Design — Ledger Lease & Safe Recovery (platform U1)

**Increment**: recovery-model change. Closes infra-increment code-review finding [1] / production-readiness limitation 1 (split-topology mid-run recovery) without the double-execution hazard.
**Decisions**: Q1=A(owner+expiry) · Q2=A(heartbeat) · Q3=A(ClaimItem) · Q4=A(ListExpiredInflight) · Q5=A(manager+all periodic) · Q6=A(running+recent list) · Q7=A(60s TTL).

---

## E-L1. Entities

**Lease** (new value type, `platform`):
```
Lease {
    Owner  string            // claiming worker/consumer id (hostname+goroutine)
    Expiry domain.Timestamp  // unix ms; item is reclaimable once now > Expiry
}
```

**WorkLedgerEntry** gains two persisted fields (Q1=A):
```
WorkLedgerEntry {
    ID, ScanID, Item, State, Outcome   // existing
    LeaseOwner  string            (bson:"leaseOwner")   // "" when pending/done
    LeaseExpiry domain.Timestamp  (bson:"leaseExpiry")  // 0 when pending/done
}
```
Both cleared (owner="", expiry=0) whenever the item leaves inflight (reclaimed→pending, or →done).

Constants: `LeaseTTL = 60s` (Q7=A), `HeartbeatInterval = 20s` (< TTL/2, so two refreshes fit before expiry). Clock is the injected `Clock` (already in orchestrator Config).

---

## E-L2. Seam evolution (TaskStore)

The recovery model is a functional change, so the seam evolves (both mem + Mongo implement; not an infra-swap so PR-11 "invariant during swap" is not violated — but changes are minimized). Four additions/changes:

```go
// Atomic claim + lease stamp: pending→inflight AND set (owner, expiry) in one op.
ClaimItem(ctx, wid, lease Lease) (bool, error)                 // NEW (replaces the bare pending→inflight CAS in the worker path)

// Heartbeat: extend the lease iff still inflight AND owned by `owner`. Returns
// false if the item was reclaimed/stolen (owner mismatch or no longer inflight)
// — the worker uses that to ABORT (see BR-L3).
RefreshLease(ctx, wid, owner string, newExpiry domain.Timestamp) (bool, error)  // NEW

// Enumerate a scan's inflight items whose lease has expired (sweep targets).
ListExpiredInflight(ctx, id, now domain.Timestamp) ([]domain.WorkItem, error)   // NEW (Q4=A)

// Record the outcome ONLY if the caller still owns the inflight lease — prevents
// a reclaimed straggler from overwriting the re-run worker's result.
RecordOutcome(ctx, wid, owner string, o domain.WorkItemOutcome) (bool, error)   // owner+bool ADDED
```
`CompareAndSetState`, `CompareAndSetItemState` (still used by the sweep's reset inflight→pending and by finalize), `ListUnfinished`, `AllItems`, `AllOutcomes`, `SaveReport`, `LoadReport`, `ListScansByState`, `SaveScan`, `LoadScan`, `AppendWorkItems` — unchanged.

Mongo index addition: `{scanId:1, state:1, leaseExpiry:1}` so `ListExpiredInflight` is index-served.

---

## E-L3. Business rules (the lease protocol)

- **BR-L1 (claim stamps lease)**: a worker claims via `ClaimItem(wid, Lease{Owner:self, Expiry:now+TTL})` — atomic `{state:pending} → {state:inflight, lease}`. Exactly one concurrent claimer wins (same single-winner property as before, PR-4).
- **BR-L2 (heartbeat refresh)**: while running the checker, the worker refreshes every `HeartbeatInterval` via `RefreshLease(wid, self, now+TTL)`. It succeeds only while the item is inflight AND `LeaseOwner==self`.
- **BR-L3 (abort on lost lease)**: if a `RefreshLease` returns false (the sweep reclaimed the item because this worker stalled/was presumed dead), the worker **cancels its work ctx and does not record an outcome**. This is the coordination point that makes reclaim safe even though the sweep's reset is not atomic-with-expiry.
- **BR-L4 (safe reclaim, sweep)**: the control-plane sweep lists `ListExpiredInflight(scan, now)` and for each resets `CompareAndSetItemState(wid, inflight, pending)` (which also clears the lease) then re-enqueues it. An expired lease means no live worker refreshed in time → reclaim is safe; a merely-slow worker that comes back is stopped by BR-L3.
- **BR-L5 (owner-guarded outcome)**: `RecordOutcome(wid, self, outcome)` writes `done` only if the item is still inflight AND owned by `self`. A reclaimed straggler's write is rejected (returns false) — so two workers can never both finalize one item.
- **Safe-reclaim invariant (the property this increment guarantees)**: for any item, **at most one worker's outcome is ever recorded**, even under arbitrary interleavings of claim / expire / reclaim / re-claim. Double *starting* is possible for a brief window; double *recording* (and thus double-counting in coverage) is not.

---

## E-L4. Recovery sweep (Q5=A)

- Runs in the **manager** and **all** roles (not worker — control-plane responsibility). Both:
  - **boot**: one immediate sweep (recovers prior-crash orphans; generalizes the old `recoverOrphans`).
  - **periodic**: every 30s, `for each running scan: ListExpiredInflight → reset+re-enqueue` (BR-L4). Now SAFE (lease-gated), so the periodic sweep removed in the infra increment is restored.
- Uses `ListScansByState(running)` (already exists) to enumerate scans.
- Replaces `cmd/chiller/recoverOrphans` (boot-only/all-only) with a shared sweep used by both roles.

---

## E-L5. Dashboard scan list (Q6=A)

- New authz'd endpoint `GET /api/introspect/scans`: returns running scans + a bounded number of recently-terminated (completed/stopped/failed), each with its `ProgressSnapshot` (Total/Done/Failed/Inflight + per-checker). Built from `ListScansByState` per state + `Orchestrator.GetStatus`.
- UI: a scan table above the existing queue cards; the id-lookup box stays.
- Security: same authz fail-closed (SECURITY-08) + the HTML security headers already applied to `/ui` (SECURITY-04).

---

## E-L6. Testable properties (PBT-01)

| Property | Category | Test |
|---|---|---|
| Concurrent `ClaimItem` on one pending item → exactly one winner | invariant | contract suite (mem + Mongo) |
| `RefreshLease` succeeds ⇔ inflight ∧ owner matches; fails after reclaim | invariant | contract |
| Random interleaving of claim/expire/reclaim/re-claim/record → **at most one recorded outcome** per item (safe-reclaim invariant) | oracle/stateful (PBT-05/06) | model-based test: N workers + a sweeper as a command sequence vs a reference model; assert no item is recorded twice |
| `ListExpiredInflight` returns exactly inflight items with `expiry<now` | invariant | contract |
| Lease round-trips through BSON (owner+expiry) | round-trip (PBT-02) | serialization test |

---

## Extension compliance (this design)

| Rule | Verdict | Basis |
|---|---|---|
| SECURITY-08 (authz) | **Compliant** | new `/api/introspect/scans` behind the same fail-closed Bearer guard |
| SECURITY-04 (headers) | **Compliant** | UI additions reuse the existing HTML security-header middleware |
| SECURITY-15 (fail-closed) | **Compliant** | BR-L3 aborts on lost lease; BR-L5 rejects stale writes — errors deny, never double-commit |
| RESILIENCY-06 (health) | N/A (unchanged) | — |
| RESILIENCY-10 (timeouts/bulkhead) | **Compliant** | lease TTL + heartbeat is a lease/visibility control; sweep bounded per scan |
| RESILIENCY-11/13 (DR/failover) | **Compliant** | this IS the failover-recovery mechanism; boot + periodic sweep documented |
| PBT-01/05/06 | **Compliant (design)** | safe-reclaim invariant carried to Code Gen as a stateful model test |
| PBT-02 | **Compliant (design)** | lease BSON round-trip |
| others | N/A / carried to Code Gen | — |

**Blocking findings: 0.**

---

## Deferred / honest
- Double *starting* (not recording) is possible in a narrow window (worker stalls past TTL, sweep reclaims, worker resumes and aborts at next heartbeat). For a passive scanner this means at most a few extra rate-limited GETs before the straggler aborts — bounded and acceptable; eliminating it entirely would need fencing tokens on every outbound call (out of scope).
- Lease clock skew across processes: TTL (60s) ≫ expected skew; if deploying across hosts with unsynced clocks, use NTP (documented for Operations).
