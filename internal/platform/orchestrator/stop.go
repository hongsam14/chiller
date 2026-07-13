package orchestrator

import (
	"context"
	"fmt"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/platform"
)

// StopScan stops a scan (T3/T4, US-…): it CAS-transitions running→stopped (or
// created→stopped), drains the scan's undispatched queue items, and cancels the
// scan-scoped ctx so in-flight fetches abort promptly (H4). In-flight items that
// have already been claimed finish naturally or are cut off by the ctx cancel.
func (o *Orchestrator) StopScan(ctx context.Context, id domain.ScanID) error {
	s, err := o.cfg.Store.LoadScan(ctx, id)
	if err != nil {
		return fmt.Errorf("%w: %v", platform.ErrNotFound, err)
	}
	switch s.State {
	case platform.StateRunning:
		if ok, err := o.cfg.Store.CompareAndSetState(ctx, id, platform.StateRunning, platform.StateStopped); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("stop: scan %q changed state concurrently", id)
		}
	case platform.StateCreated:
		if ok, err := o.cfg.Store.CompareAndSetState(ctx, id, platform.StateCreated, platform.StateStopped); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("stop: scan %q changed state concurrently", id)
		}
	default:
		return fmt.Errorf("stop: scan %q is %s (terminal)", id, s.State)
	}

	_ = o.cfg.Queue.Drain(ctx, id) // drop undispatched items
	o.cancelScan(id)               // cancel in-flight fetches
	o.wipeCache(ctx, id)           // a stopped scan leaves the same bodies behind as a completed one (FR-12)
	return nil
}

// cancelScan cancels a scan's scoped ctx if present.
func (o *Orchestrator) cancelScan(id domain.ScanID) {
	o.mu.Lock()
	cancel := o.cancelByID[id]
	o.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
