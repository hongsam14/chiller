package orchestrator

import (
	"context"
	"fmt"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/platform"
)

// ResumeScan re-enqueues ALL of a running scan's unfinished items (T5, US-025),
// used at boot to repopulate the queue after a restart. It does NOT reset item
// state: a pending item re-claims normally, and a crash-orphaned inflight item is
// re-taken by a worker via ClaimItem's steal-once-expired path (BR-L4), so this
// is safe to call even on a live scan — a still-live worker's item (lease not
// expired) simply cannot be stolen and the duplicate queue entry is dropped.
// Re-enqueue is best-effort per item (an Enqueue error does not strand the rest);
// an item that fails to enqueue stays unfinished and is recovered by the next
// boot/sweep.
func (o *Orchestrator) ResumeScan(ctx context.Context, id domain.ScanID) error {
	s, err := o.cfg.Store.LoadScan(ctx, id)
	if err != nil {
		return fmt.Errorf("%w: %v", platform.ErrNotFound, err)
	}
	if s.State != platform.StateRunning {
		return fmt.Errorf("resume: scan %q is %s, not running", id, s.State)
	}
	// Rebuild the per-scan executor if this process lost it (e.g. restart).
	if _, _, ok := o.executorFor(id); !ok {
		set, err := o.cfg.Ingestor.Ingest(s.Request)
		if err != nil {
			return fmt.Errorf("%w: %v", platform.ErrBadRequest, err)
		}
		o.buildExecutor(id, s.Request, set.Options)
	}

	unfinished, err := o.cfg.Store.ListUnfinished(ctx, id)
	if err != nil {
		return err
	}
	return o.reEnqueue(ctx, unfinished)
}

// ReclaimExpired is the SAFE periodic-sweep primitive (BR-L4): it re-enqueues
// ONLY inflight items whose lease has expired (worker missed its heartbeats,
// presumed dead). It does NOT reset state — a worker takes the item via
// ClaimItem's atomic steal-once-expired, so a live worker whose heartbeat pushed
// the lease into the future is never disturbed (that atomicity is what fixes the
// list-then-reset TOCTOU). Because the item stays inflight until actually stolen,
// a failed Enqueue simply leaves it for the next sweep to re-find (self-healing).
// Returns the number of expired items re-enqueued.
func (o *Orchestrator) ReclaimExpired(ctx context.Context, id domain.ScanID) (int, error) {
	expired, err := o.cfg.Store.ListExpiredInflight(ctx, id, o.cfg.Clock())
	if err != nil {
		return 0, err
	}
	return len(expired), o.reEnqueue(ctx, expired)
}

// reEnqueue best-effort enqueues items, continuing past a per-item error so one
// transient Enqueue failure never strands the rest of the batch (returns the
// first error for logging).
func (o *Orchestrator) reEnqueue(ctx context.Context, items []domain.WorkItem) error {
	var firstErr error
	for _, it := range items {
		if err := o.cfg.Queue.Enqueue(ctx, it); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
