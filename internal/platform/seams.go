package platform

import (
	"context"
	"net/http"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/findings/report"
)

// TaskStore persists Scan aggregates and their WorkItem ledger (E-5). MVP:
// in-memory map. Infra: Mongo — same interface. All methods carry ctx (network
// cancel/deadline) and return error; CAS returns (swapped, error); values are
// returned by copy so the store never leaks mutable internal state (E-6).
type TaskStore interface {
	SaveScan(ctx context.Context, s Scan) error
	LoadScan(ctx context.Context, id domain.ScanID) (Scan, error)
	CompareAndSetState(ctx context.Context, id domain.ScanID, expected, next ScanState) (bool, error)
	AppendWorkItems(ctx context.Context, id domain.ScanID, items []domain.WorkItem) error
	// CompareAndSetItemState transitions an item's state (still used for the
	// finalize path and the sweep's inflight→pending reclaim reset, which also
	// clears the lease). Claiming is done via ClaimItem so the lease is stamped
	// atomically with the pending→inflight transition.
	CompareAndSetItemState(ctx context.Context, wid domain.WorkItemID, expected, next ItemState) (bool, error)
	// ClaimItem atomically takes an item and stamps the lease (owner + expiry),
	// winning if the item is pending OR is inflight with an EXPIRED lease
	// (leaseExpiry < now) — i.e. it also STEALS a dead worker's expired item
	// atomically (BR-L1/L4). The atomic leaseExpiry<now guard is what guarantees a
	// live worker's item (whose heartbeat pushed leaseExpiry into the future) is
	// never stolen. Exactly one concurrent claimer wins (PR-4).
	ClaimItem(ctx context.Context, wid domain.WorkItemID, lease Lease, now domain.Timestamp) (bool, error)
	// RefreshLease extends an inflight item's lease, but ONLY while it is still
	// inflight and owned by `owner` (BR-L2). A false return tells a heartbeating
	// worker its item was reclaimed — it must then abort (BR-L3).
	RefreshLease(ctx context.Context, wid domain.WorkItemID, owner string, newExpiry domain.Timestamp) (bool, error)
	// ListExpiredInflight returns a scan's inflight items whose lease has expired
	// (now > LeaseExpiry) — the recovery sweep's reclaim targets (BR-L4, Q4=A).
	ListExpiredInflight(ctx context.Context, id domain.ScanID, now domain.Timestamp) ([]domain.WorkItem, error)
	// RecordOutcome finalizes an item (→done) with its outcome, but ONLY if it is
	// still inflight and owned by `owner` (BR-L5). A false return means the item
	// was reclaimed and re-run elsewhere — the caller must not double-count it.
	RecordOutcome(ctx context.Context, wid domain.WorkItemID, owner string, o domain.WorkItemOutcome) (bool, error)
	// ListUnfinished returns pending ∪ inflight items (crash-orphaned inflight
	// included) — the CAS-resume source of truth.
	ListUnfinished(ctx context.Context, id domain.ScanID) ([]domain.WorkItem, error)
	// CountUnfinished returns |pending ∪ inflight| for a scan — the O(1)/indexed
	// finalize trigger (== 0 means the ledger drained), avoiding an O(N) fetch per
	// item completion on the worker hot path.
	CountUnfinished(ctx context.Context, id domain.ScanID) (int, error)
	// AllItems returns every fan-out WorkItem of a scan (any state) — the fold
	// uses each item's Scope to know which checkers ran on it.
	AllItems(ctx context.Context, id domain.ScanID) ([]domain.WorkItem, error)
	AllOutcomes(ctx context.Context, id domain.ScanID) ([]domain.WorkItemOutcome, error)
	// PurgeOutcomeEvidence drops the evidence the ledger's outcomes are still
	// carrying for a scan, leaving the rest of each outcome intact. The ledger is
	// how a worker's findings reach the manager, so it holds a copy of every
	// response fragment until the scan finalizes — after which nothing reads it
	// again. finalize calls this once the findings are durably stored, so a
	// completed scan's evidence lives in exactly one place (data-census O-2).
	//
	// It is safe to call when there is nothing to purge, and it must NOT be called
	// before the findings are stored: the ledger is the only other copy.
	PurgeOutcomeEvidence(ctx context.Context, id domain.ScanID) error
	// ListScansByState returns the ids of scans in a given state. The recovery
	// sweep uses it (state=running) to drive ResumeScan across process restarts;
	// the durable backend answers it from the {state} index.
	ListScansByState(ctx context.Context, state ScanState) ([]domain.ScanID, error)
	SaveReport(ctx context.Context, id domain.ScanID, r report.Report) error
	LoadReport(ctx context.Context, id domain.ScanID) (report.Report, bool, error)
}

// WorkQueue distributes WorkItems to workers (E-5). MVP: buffered channel.
// Infra: Redis/Valkey BLPOP with visibility — same interface. Dequeue blocks on
// ctx; at-least-once delivery is made exactly-once-effective by the CAS claim in
// the ledger (PR-4), so the queue itself need not be transactional.
type WorkQueue interface {
	Enqueue(ctx context.Context, item domain.WorkItem) error
	Dequeue(ctx context.Context) (domain.WorkItem, error) // blocks until item or ctx done
	Drain(ctx context.Context, id domain.ScanID) error    // drop a scan's undispatched items
}

// QueueInspector is an OPTIONAL, read-only introspection capability a WorkQueue
// impl MAY additionally provide for the ops dashboard. It is deliberately a
// SEPARATE interface from WorkQueue so the 3-method work contract stays invariant
// (PR-11); the UI layer type-asserts for it and degrades gracefully when it is
// absent. It never affects dispatch.
type QueueInspector interface {
	Stats(ctx context.Context) (QueueStats, error)
}

// QueueStats is the introspection snapshot rendered by the dashboard.
type QueueStats struct {
	Depth     int            // undelivered entries (XLEN; mem: queue length)
	Pending   int            // delivered-but-unacked entries (PEL); mem: 0
	Drained   int            // number of drained scans
	Reclaimed int64          // cumulative reaper reclaims; mem: 0
	Consumers []ConsumerStat // per-consumer pending (Valkey only)
}

// ConsumerStat is one consumer's pending backlog in a QueueStats.
type ConsumerStat struct {
	Name    string
	Pending int
}

// ProgressReporter is the live progress read model (E-5). MVP: in-memory
// counters. Infra: + OTel exporter behind the same interface.
type ProgressReporter interface {
	Init(ctx context.Context, id domain.ScanID, total int) error
	// OnClaimed takes the WorkItem (not just its id) so the reporter can
	// attribute the in-flight increment to the item's scan without a separate
	// wid→scan index — equally natural for the in-memory and OTel impls.
	OnClaimed(ctx context.Context, item domain.WorkItem) error
	// OnReleased undoes an OnClaimed's inflight increment WITHOUT counting the
	// item as done/failed — used when a worker loses its lease (item reclaimed and
	// re-run elsewhere), so the inflight gauge does not leak (BR-L5).
	OnReleased(ctx context.Context, item domain.WorkItem) error
	OnFinished(ctx context.Context, id domain.ScanID, o domain.WorkItemOutcome) error
	Snapshot(ctx context.Context, id domain.ScanID) (ProgressSnapshot, error)
}

// Authorizer gates the ScanAPI (E-5). MVP: static Bearer token, fail-closed.
// Infra: OIDC/mTLS. The request carries its own ctx, so no separate ctx arg.
type Authorizer interface {
	Authorize(r *http.Request) error // nil = allow; non-nil = deny (fail-closed)
}

// BrowserSession owns THE single authenticated browser context (storageState
// injected) for financial mode (PR-12, Q1=B). MVP: Playwright/Chromium. Test:
// fakeSession. Concurrent requests are bounded by the WORK tab pool of size T;
// the login keep-alive tab is reserved separately and excluded from T.
type BrowserSession interface {
	// Do injects the authenticated session's cookies/headers and issues the
	// request to the target, returning the response. It blocks until a work-pool
	// tab slot (of T) is free.
	Do(ctx context.Context, req *http.Request) (*http.Response, error)
	Close(ctx context.Context) error
}

// AuthProxy is the local forward proxy workers egress through in financial mode
// (PR-12, Q3=B). It receives a worker's outbound request and re-issues it via
// BrowserSession (session injected), preserving the single-session invariant as
// workers scale horizontally. MVP: HTTP absolute-form forwarding; HTTPS MITM is
// deferred to Infra.
type AuthProxy interface {
	Serve(ctx context.Context, addr string) error // blocks; returns when ctx is done
}

// Orchestrator is the control-plane facade the ScanAPI drives (E-5). Pure Go
// over the injected seams + the scan Executor + the findings pipeline; it is
// backend-agnostic and never changes when a seam impl is swapped.
type Orchestrator interface {
	CreateScan(ctx context.Context, req ScanRequest, fw ...domain.Framework) (domain.ScanID, error)
	StartScan(ctx context.Context, id domain.ScanID) error
	ResumeScan(ctx context.Context, id domain.ScanID) error
	StopScan(ctx context.Context, id domain.ScanID) error
	GetStatus(ctx context.Context, id domain.ScanID) (ProgressSnapshot, error)
	GetReport(ctx context.Context, id domain.ScanID) (report.Report, bool, error)
}
