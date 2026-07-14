// Package progress is the MVP in-memory ProgressReporter (Q5=A, US-028): live
// per-scan counters exposed through the ScanAPI status endpoint. An OTel
// exporter is added at Infra behind the same interface (the reporter is the only
// place telemetry is emitted, so nothing else changes).
package progress

import (
	"context"
	"sync"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/platform"
)

// Mem holds in-memory progress counters per scan.
type Mem struct {
	mu    sync.RWMutex
	scans map[domain.ScanID]*counters
}

type counters struct {
	total, done, failed, inflight int
	perChecker                    map[domain.CheckerID]platform.CheckerCount
}

// New builds an empty progress reporter.
func New() *Mem { return &Mem{scans: map[domain.ScanID]*counters{}} }

var _ platform.ProgressReporter = (*Mem)(nil)

func (m *Mem) Init(_ context.Context, id domain.ScanID, total int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scans[id] = &counters{total: total, perChecker: map[domain.CheckerID]platform.CheckerCount{}}
	return nil
}

func (m *Mem) OnClaimed(_ context.Context, item domain.WorkItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c := m.scans[item.ScanID]; c != nil {
		c.inflight++
	}
	return nil
}

// OnReleased undoes an OnClaimed increment when a worker loses its lease (the
// item was reclaimed and will be re-run elsewhere), without counting it as
// done/failed — so the inflight gauge does not leak (BR-L5).
func (m *Mem) OnReleased(_ context.Context, item domain.WorkItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c := m.scans[item.ScanID]; c != nil && c.inflight > 0 {
		c.inflight--
	}
	return nil
}

func (m *Mem) OnFinished(_ context.Context, id domain.ScanID, o domain.WorkItemOutcome) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.scans[id]
	if c == nil {
		return nil
	}
	if c.inflight > 0 {
		c.inflight--
	}
	// An item is Failed (as a whole) iff at least one of its checkers errored;
	// otherwise Done. Done ∪ Failed partitions the finished items (E-4).
	if len(o.Failed) > 0 {
		c.failed++
	} else {
		c.done++
	}
	for _, cid := range o.Failed {
		cc := c.perChecker[cid]
		cc.Failed++
		c.perChecker[cid] = cc
	}
	for cid := range okCheckerIDs(o) {
		cc := c.perChecker[cid]
		cc.OK++
		c.perChecker[cid] = cc
	}
	return nil
}

func (m *Mem) Snapshot(_ context.Context, id domain.ScanID) (platform.ProgressSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snap := platform.ProgressSnapshot{ScanID: id, PerChecker: map[domain.CheckerID]platform.CheckerCount{}}
	c := m.scans[id]
	if c == nil {
		return snap, nil
	}
	snap.Total, snap.Done, snap.Failed, snap.Inflight = c.total, c.done, c.failed, c.inflight
	for cid, cc := range c.perChecker {
		snap.PerChecker[cid] = cc
	}
	return snap, nil
}

// okCheckerIDs returns checkers that produced a non-error result on an item —
// those recording a not-applicable judgement — minus any that failed. (Finding
// sources are not counted here: a finding carries a SourceTool string, not the
// owning CheckerID, so per-checker OK is derived from the structural signals the
// outcome carries by CheckerID.)
func okCheckerIDs(o domain.WorkItemOutcome) map[domain.CheckerID]struct{} {
	failed := map[domain.CheckerID]bool{}
	for _, f := range o.Failed {
		failed[f] = true
	}
	ok := map[domain.CheckerID]struct{}{}
	for cid := range o.NotApplicable {
		if !failed[cid] {
			ok[cid] = struct{}{}
		}
	}
	return ok
}
