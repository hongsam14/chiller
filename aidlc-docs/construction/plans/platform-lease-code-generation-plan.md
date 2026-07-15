# Code Generation Plan — platform (U1) Ledger Lease

**Stage**: CONSTRUCTION → Code Generation (platform U1 lease increment)
**Design input**: `construction/platform/functional-design/lease-recovery.md` (E-L1..L6, BR-L1..L5, safe-reclaim invariant).
**Decisions**: Q1=A owner+expiry · Q2=A heartbeat · Q3=A ClaimItem · Q4=A ListExpiredInflight · Q5=A manager+all periodic · Q6=A running+recent list · Q7=A 60s TTL.
**Single source of truth** for this increment.

## Code location (workspace root)
`internal/platform/{types.go,seams.go}` · `internal/platform/taskstore/{taskstore.go,taskstore_mongo.go,contract_test.go,*_integration}` · `internal/platform/orchestrator/{worker.go,resume.go,orchestrator.go}` · `internal/platform/api/{introspect.go}` · `cmd/chiller/main.go`. Docs: `construction/platform/code/`.

## Steps

### Step 1 — Entities + seam (types.go, seams.go)
- [x] 1a: `Lease{Owner string; Expiry domain.Timestamp}` value type; `LeaseTTL=60s`, `HeartbeatInterval=20s` consts.
- [x] 1b: `WorkLedgerEntry` += `LeaseOwner string` (bson `leaseOwner`) + `LeaseExpiry domain.Timestamp` (bson `leaseExpiry`).
- [x] 1c: `TaskStore` seam: add `ClaimItem(ctx,wid,lease Lease)(bool,error)`, `RefreshLease(ctx,wid,owner string,newExpiry domain.Timestamp)(bool,error)`, `ListExpiredInflight(ctx,id domain.ScanID,now domain.Timestamp)([]domain.WorkItem,error)`; change `RecordOutcome` → `RecordOutcome(ctx,wid,owner string,o domain.WorkItemOutcome)(bool,error)` (BR-L5 owner guard).

### Step 2 — mem impl (taskstore.go)
- [x] 2a: `ClaimItem` — under lock: if entry pending → set inflight + lease, return true; else false. Clears no lease on fail.
- [x] 2b: `RefreshLease` — true iff inflight && LeaseOwner==owner; bump LeaseExpiry. else false.
- [x] 2c: `ListExpiredInflight` — entries where state==inflight && LeaseExpiry<now.
- [x] 2d: `RecordOutcome(owner)` — only if inflight && owner matches → set done + clear lease + store outcome, return true; else false (stale straggler rejected).
- [x] 2e: the sweep's reset path: `CompareAndSetItemState(inflight,pending)` must also clear lease (owner="",expiry=0).

### Step 3 — Mongo impl (taskstore_mongo.go)
- [x] 3a: `ClaimItem` = `findOneAndUpdate({_id,state:pending},{$set:{state:inflight,leaseOwner,leaseExpiry}})` → matched? bool.
- [x] 3b: `RefreshLease` = `findOneAndUpdate({_id,state:inflight,leaseOwner:owner},{$set:{leaseExpiry}})` → bool.
- [x] 3c: `ListExpiredInflight` = `Find({scanId,state:inflight,leaseExpiry:{$lt:now}})`.
- [x] 3d: `RecordOutcome(owner)` = `findOneAndUpdate({_id,state:inflight,leaseOwner:owner},{$set:{state:done,outcome},$unset lease})` → bool.
- [x] 3e: `CompareAndSetItemState` inflight→pending also `$set:{leaseOwner:"",leaseExpiry:0}`.
- [x] 3f: index `{scanId:1,state:1,leaseExpiry:1}` in `ensureIndexes`.

### Step 4 — Orchestrator (worker.go, resume.go)
- [x] 4a: `workerLoop`/`processItem`: claim via `ClaimItem(item.ID, Lease{owner, clock()+TTL})` (owner = process id + goroutine seq). On false → skip.
- [x] 4b: heartbeat goroutine every HeartbeatInterval: `RefreshLease(owner, clock()+TTL)`; on false → cancel the work ctx (BR-L3 abort).
- [x] 4c: after `executor.Run`: `ok,_ := RecordOutcome(item.ID, owner, outcome)`; only if ok call `Progress.OnFinished` + finalize check (BR-L5 — a rejected write means the item was reclaimed and re-run elsewhere; do not double-count).
- [x] 4d: `resume.go` ResumeScan: the inflight→pending reset (already there) now also clears lease via the updated CAS — no logic change beyond that.

### Step 5 — Recovery sweep (cmd/chiller or orchestrator)
- [x] 5a: replace `recoverOrphans` with `runRecoverySweep(ctx, orch, store, clock, every)` — boot sweep + ticker(30s); each tick: `ListScansByState(running)` → per scan `ListExpiredInflight(now)` → for each `CompareAndSetItemState(inflight,pending)` (clears lease) + `Queue.Enqueue`. (Reuse ResumeScan? ResumeScan re-enqueues ALL unfinished incl. live inflight — NOT safe for periodic. The sweep must re-enqueue only EXPIRED ones. So the sweep drives ListExpiredInflight directly, not ResumeScan.)
- [x] 5b: wire in **all** and **manager** roles (boot + periodic). worker role: none.

### Step 6 — Dashboard scan list (api/introspect.go)
- [x] 6a: `GET /api/introspect/scans` (authz) — `ListScansByState` for running + bounded recent (completed/stopped/failed), each → `orch.GetStatus` ProgressSnapshot. Needs the Handler to hold a TaskStore + Orchestrator (orch already held; add store via option).
- [x] 6b: UI: scan table (id, state, done/total, failed, inflight) polling `/api/introspect/scans`; keep id-lookup + queue cards. Security headers unchanged.
- [x] 6c: unit tests (authz, payload).

### Step 7 — Tests
- [x] 7a: contract suite: `ClaimItem` single-winner (replaces the CAS claim test), `RefreshLease` owner-guard, `ListExpiredInflight`, `RecordOutcome` owner-guard rejects stale. Update existing RecordOutcome callers to the new signature.
- [x] 7b: **PBT safe-reclaim** (PBT-05/06): command sequence {claim, expire (advance clock), sweep-reclaim, re-claim, record} across items vs a reference model; assert **no item recorded twice** and unfinished/outcome counts match the model.
- [x] 7c: integration files still compile (RecordOutcome/ClaimItem exercised via contract).

### Step 8 — Verify + docs
- [x] 8a: `go build/vet`, `gofmt -l`, `make arch-check`, `go test ./...`, `go test -race ./internal/platform/... ./test/`.
- [x] 8b: `construction/platform/code/lease-business-logic-summary.md`; update `operations/production-readiness.md` limitation 1 → resolved (split-topology recovery now safe).

## Extension compliance (gen)
PBT FULL: 7b safe-reclaim stateful test is mandatory (blocking). SECURITY-08 (scans endpoint authz), SECURITY-15 (BR-L3/BR-L5 fail-closed). RESILIENCY-11/13 (this is the recovery mechanism).

## Impact / size
Seam: 3 new methods + 1 signature change (RecordOutcome). Callers updated: worker.go, contract_test.go (5), Mongo. New: heartbeat goroutine, sweep, scans endpoint+UI. Data plane (scan/findings) untouched (arch-check green). `RecordOutcome` bool return lets the worker skip double-counting.
