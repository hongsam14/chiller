// Package workqueue is the MVP in-process WorkQueue (Q1/Q2=A, PR-3a): a
// mutex-guarded FIFO with a coalesced wakeup signal, so Dequeue blocks on ctx
// (matching a Redis BLPOP contract) and Drain can selectively drop one scan's
// undispatched items. The Redis/Valkey implementation is added at Infra behind
// the identical interface.
package workqueue

import (
	"context"
	"sync"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/platform"
)

// Mem is a non-durable in-process WorkQueue. It uses an internally growable
// slice (never a fixed channel buffer) so a large fan-out enqueued before any
// worker drains it cannot deadlock — important for the manager/worker split.
type Mem struct {
	mu      sync.Mutex
	items   []domain.WorkItem
	drained map[domain.ScanID]bool
	// notify is a coalesced wakeup: Enqueue sends a token (non-blocking, so at
	// most one is buffered), Dequeue waits on it only after finding the queue
	// empty under the lock. Because a waiter always drains all eligible items
	// under the lock before waiting again, one coalesced token per burst suffices.
	notify chan struct{}
}

// New builds an empty in-process WorkQueue.
func New() *Mem {
	return &Mem{drained: map[domain.ScanID]bool{}, notify: make(chan struct{}, 1)}
}

var _ platform.WorkQueue = (*Mem)(nil)
var _ platform.QueueInspector = (*Mem)(nil)

// Stats reports what the in-process queue can see (QueueInspector): depth and the
// count of drained scans. It has no delivered-but-unacked concept, so Pending and
// Reclaimed are always 0.
func (m *Mem) Stats(_ context.Context) (platform.QueueStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	drained := 0
	for _, d := range m.drained {
		if d {
			drained++
		}
	}
	return platform.QueueStats{Depth: len(m.items), Drained: drained}, nil
}

func (m *Mem) Enqueue(_ context.Context, item domain.WorkItem) error {
	m.mu.Lock()
	if m.drained[item.ScanID] {
		m.mu.Unlock()
		return nil // scan was stopped; drop silently
	}
	m.items = append(m.items, item)
	m.mu.Unlock()
	m.signal()
	return nil
}

func (m *Mem) Dequeue(ctx context.Context) (domain.WorkItem, error) {
	for {
		m.mu.Lock()
		if i := m.firstEligible(); i >= 0 {
			it := m.items[i]
			m.items = append(m.items[:i], m.items[i+1:]...)
			m.mu.Unlock()
			return it, nil
		}
		m.mu.Unlock()

		select {
		case <-ctx.Done():
			return domain.WorkItem{}, ctx.Err()
		case <-m.notify:
			// re-check under the lock on the next iteration
		}
	}
}

func (m *Mem) Drain(_ context.Context, id domain.ScanID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drained[id] = true
	kept := m.items[:0]
	for _, it := range m.items {
		if it.ScanID != id {
			kept = append(kept, it)
		}
	}
	m.items = kept
	return nil
}

// firstEligible returns the index of the first item whose scan is not drained,
// or -1. Caller holds the lock.
func (m *Mem) firstEligible() int {
	for i, it := range m.items {
		if !m.drained[it.ScanID] {
			return i
		}
	}
	return -1
}

func (m *Mem) signal() {
	select {
	case m.notify <- struct{}{}:
	default: // token already pending; coalesce
	}
}
