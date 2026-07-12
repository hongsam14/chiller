# Code Generation Summary — platform (U1) Ledger Lease

**Generated**: 2026-07-11. **Green** (build/vet/gofmt/arch-check/`go test ./...`/`-race`).
**Purpose**: close the split-topology mid-run recovery gap (infra-increment review finding [1] / production-readiness limitation 1) by making "stale inflight" decidable via a lease, so a control-plane sweep reclaims only genuinely-dead workers' items — no double-recording.

## What was built (design `lease-recovery.md`)

### Entities (types.go)
- `Lease{Owner string, Expiry domain.Timestamp}`; `WorkLedgerEntry` += `LeaseOwner`/`LeaseExpiry` (bson-tagged). `LeaseTTL=60s`, `HeartbeatInterval=20s`.

### Seam evolution (seams.go — both mem + Mongo implement)
- `ClaimItem(ctx, wid, lease) (bool,error)` — atomic pending→inflight + lease stamp (BR-L1). Replaces the bare CAS in the worker claim path.
- `RefreshLease(ctx, wid, owner, newExpiry) (bool,error)` — extends only while inflight ∧ owned (BR-L2); false ⇒ reclaimed.
- `ListExpiredInflight(ctx, id, now) ([]WorkItem,error)` — sweep targets (Q4=A).
- `RecordOutcome(ctx, wid, owner, o) (bool,error)` — signature changed: finalizes only if inflight ∧ owned (BR-L5); false ⇒ reclaimed/re-run elsewhere (caller skips double-count).
- `CompareAndSetItemState` unchanged signature, but now clears the lease when leaving inflight (used by the sweep's reset + finalize).
- Mongo: all four via `findOneAndUpdate` (atomic); index `{scanId,state,leaseExpiry}` added.

### Orchestrator (worker.go, resume.go)
- `processItem`: claim via `ClaimItem` with `Lease{owner=host-pid-wN, expiry=clock+TTL}`; spawn a **heartbeat** goroutine (`RefreshLease` every 20s) that **cancels the work ctx if a refresh fails** (BR-L3 abort); after `executor.Run`, `RecordOutcome(owner)` — only on a true return does it OnFinished + finalize (BR-L5, no double-count).
- `ReclaimExpired(ctx, id) (int,error)` — the SAFE periodic-sweep primitive: `ListExpiredInflight` → reset each inflight→pending (clears lease) → re-enqueue. Distinct from `ResumeScan` (which re-enqueues ALL unfinished; only safe at boot before workers start).

### Composition root (cmd/chiller)
- `all`: boot `recoverOrphans` (full ResumeScan, immediate — safe pre-worker-start) **+** `startExpiredSweep` (30s, lease-safe) after workers start.
- `manager`: `startExpiredSweep` (30s) — **now safely recovers a dead worker's items mid-run without clobbering a live worker's** (expired-only). This closes the split-topology gap.

### Dashboard (api)
- `GET /api/introspect/scans` (authz, SECURITY-08) — running + ≤20 recent terminated scans, each with ProgressSnapshot. `/ui` gains a scans table (dep-free, existing security headers).

## Safe-reclaim invariant (the property this increment guarantees)
For any item, **at most one worker's outcome is ever recorded**, under any interleaving of claim / expiry / reclaim / re-claim / stale-record. Enforced by BR-L3 (worker aborts on lost lease) + BR-L5 (owner-guarded record). Double *starting* (not recording) is possible in a narrow window (stalled worker past TTL, reclaimed, aborts at next heartbeat) — bounded, acceptable for a passive scanner.

## Tests
- Contract suite (mem + Mongo integration) extended: `Claim_SingleWinner` (32 concurrent, exactly 1), `LeaseRefreshOwnerGuard`, `ListExpiredInflight`, `RecordOutcomeOwnerGuard`, and **`PBT_SafeReclaimInvariant`** — a 1200-step command sequence (claim / advance-clock / sweep-reclaim / record-correct-owner / record-stale-owner) asserting no item is recorded twice and the store's accept/reject matches the reference model. Existing tests updated to the lease-aware claim/record. New `TestIntrospectScans_ListsRunning`.
- Verified: `go build/vet`, `gofmt -l` clean, `make arch-check` OK, `go test ./...` pass, `go test -race ./internal/platform/... ./test/` clean. Integration compiles under `-tags integration`.

## Deferred / honest
- Fully eliminating the double-START window would need fencing tokens on every outbound call (out of scope).
- Multi-host clock skew ≪ 60s TTL; NTP for split deployments (Operations).
- Integration suite still requires Docker to execute (compiles clean).
