package orchestrator

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/platform"
)

// Start launches the worker pool (W goroutines, PR-3a). Each worker pulls from
// the queue, claims via ClaimItem (stamping a lease), heartbeats the lease while
// it runs the checker, records the outcome (owner-guarded), and triggers finalize
// when its scan's ledger drains. The pool stops when ctx is cancelled. In the
// manager-only role Start is not called (no local workers).
func (o *Orchestrator) Start(ctx context.Context) {
	host, _ := os.Hostname()
	pid := os.Getpid()
	for i := 0; i < o.cfg.Workers; i++ {
		owner := fmt.Sprintf("%s-%d-w%d", host, pid, i) // unique per worker goroutine
		go o.workerLoop(ctx, owner)
	}
}

func (o *Orchestrator) workerLoop(ctx context.Context, owner string) {
	for {
		item, err := o.cfg.Queue.Dequeue(ctx)
		if err != nil {
			return // ctx cancelled: pool shutdown
		}
		o.processItem(ctx, owner, item)
	}
}

func (o *Orchestrator) processItem(ctx context.Context, owner string, item domain.WorkItem) {
	// Claim (or steal an expired-lease item) + lease stamp in one atomic op
	// (BR-L1/L4). Exactly one worker wins; a live worker's item is never stolen.
	now := o.cfg.Clock()
	claimed, err := o.cfg.Store.ClaimItem(ctx, item.ID, platform.Lease{Owner: owner, Expiry: now + o.leaseTTL()}, now)
	if err != nil || !claimed {
		return // already claimed, still-live inflight, done, or gone
	}
	_ = o.cfg.Progress.OnClaimed(ctx, item)

	executor, scanCtx, ok := o.executorFor(item.ScanID)
	if !ok {
		scanCtx = ctx // defensive: no per-scan ctx (shouldn't happen after StartScan)
	}
	// Run under the scan-scoped ctx so StopScan cancels in-flight fetches (H4),
	// and let the heartbeat cancel it too if this worker loses the lease.
	runCtx, cancel := mergeDone(ctx, scanCtx)
	defer cancel()

	// Heartbeat: refresh the lease while working; if a refresh fails the item was
	// reclaimed (this worker was presumed dead), so abort the work (BR-L3).
	hbDone := make(chan struct{})
	go o.heartbeat(runCtx, cancel, owner, item.ID, hbDone)

	outcome := executor.Run(runCtx, item, o.cfg.Cache) // per-checker fault isolation lives in exec
	close(hbDone)                                      // stop the heartbeat before recording

	// Record only if we still own the inflight lease (BR-L5). A false return means
	// the item was reclaimed and re-run elsewhere — release our inflight increment
	// (so the gauge does not leak) and do NOT record or double-count.
	recorded, _ := o.cfg.Store.RecordOutcome(ctx, item.ID, owner, outcome)
	if !recorded {
		_ = o.cfg.Progress.OnReleased(ctx, item)
		return
	}
	_ = o.cfg.Progress.OnFinished(ctx, item.ScanID, outcome)

	// O(1)/indexed drain check (not an O(N) ListUnfinished per completion).
	if n, err := o.cfg.Store.CountUnfinished(ctx, item.ScanID); err == nil && n == 0 {
		o.maybeFinalize(ctx, item.ScanID)
	}
}

// heartbeat refreshes the lease every HeartbeatInterval. A refresh that returns
// false (item no longer inflight-owned-by-us → reclaimed by the sweep) cancels
// the work ctx so the worker stops (BR-L3). It exits on ctx done or when the
// caller closes `done` (work finished).
func (o *Orchestrator) heartbeat(ctx context.Context, cancel context.CancelFunc, owner string, wid domain.WorkItemID, done <-chan struct{}) {
	t := time.NewTicker(platform.HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-t.C:
			ok, err := o.cfg.Store.RefreshLease(ctx, wid, owner, o.leaseExpiry())
			if err == nil && !ok {
				cancel() // lease lost → abort the work
				return
			}
		}
	}
}

// leaseExpiry stamps a lease TTL ahead of the injected clock (unix ms).
func (o *Orchestrator) leaseExpiry() domain.Timestamp {
	return o.cfg.Clock() + o.leaseTTL()
}

// leaseTTL is the lease validity window in the clock's unit (unix ms).
func (o *Orchestrator) leaseTTL() domain.Timestamp {
	return domain.Timestamp(platform.LeaseTTL.Milliseconds())
}

// maybeFinalize ensures a scan finalizes exactly once in-process; the terminal
// CAS in finalize is the durable guard for the multi-process (Infra) case.
func (o *Orchestrator) maybeFinalize(ctx context.Context, id domain.ScanID) {
	o.mu.Lock()
	if o.finalizing == nil {
		o.finalizing = map[domain.ScanID]bool{}
	}
	if o.finalizing[id] {
		o.mu.Unlock()
		return
	}
	o.finalizing[id] = true
	o.mu.Unlock()
	o.finalize(ctx, id)
}

// mergeDone returns a ctx cancelled when either parent is done. The returned
// cancel is context.CancelFunc — idempotent, so calling it from BOTH the deferred
// cleanup AND the heartbeat's lease-lost path is safe (no double-close panic).
// The watcher goroutine exits when b is done (cancels ctx) or when ctx is
// cancelled elsewhere.
func mergeDone(a, b context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(a)
	go func() {
		select {
		case <-b.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
