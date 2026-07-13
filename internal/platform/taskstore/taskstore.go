// Package taskstore is the MVP in-memory TaskStore (Q1=A, PR-2/11): a map+mutex
// implementation of platform.TaskStore. The durable Mongo implementation is
// added at Infra as taskstore_mongo.go beside this file, behind the identical
// interface — the orchestrator never learns which one it got.
package taskstore

import (
	"context"
	"fmt"
	"sync"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/findings/report"
	"github.com/hongsam14/chiller/internal/platform"
)

// Mem is a non-durable TaskStore for the MVP, tests, and local runs.
type Mem struct {
	mu      sync.RWMutex
	scans   map[domain.ScanID]platform.Scan
	ledger  map[domain.WorkItemID]*platform.WorkLedgerEntry
	byScan  map[domain.ScanID][]domain.WorkItemID // fan-out index for List/AllOutcomes
	notDone map[domain.ScanID]int                 // |pending ∪ inflight| for O(1) CountUnfinished
	reports map[domain.ScanID]report.Report
}

// New builds an empty in-memory TaskStore.
func New() *Mem {
	return &Mem{
		scans:   map[domain.ScanID]platform.Scan{},
		ledger:  map[domain.WorkItemID]*platform.WorkLedgerEntry{},
		byScan:  map[domain.ScanID][]domain.WorkItemID{},
		notDone: map[domain.ScanID]int{},
		reports: map[domain.ScanID]report.Report{},
	}
}

// compile-time assertion that Mem satisfies the seam.
var _ platform.TaskStore = (*Mem)(nil)

func (m *Mem) SaveScan(_ context.Context, s platform.Scan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scans[s.ID] = s
	return nil
}

func (m *Mem) LoadScan(_ context.Context, id domain.ScanID) (platform.Scan, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.scans[id]
	if !ok {
		return platform.Scan{}, fmt.Errorf("taskstore: scan %q not found", id)
	}
	return s, nil // struct copy (E-6 rule 4)
}

func (m *Mem) CompareAndSetState(_ context.Context, id domain.ScanID, expected, next platform.ScanState) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.scans[id]
	if !ok {
		return false, fmt.Errorf("taskstore: scan %q not found", id)
	}
	if s.State != expected {
		return false, nil
	}
	s.State = next
	m.scans[id] = s
	return true, nil
}

func (m *Mem) AppendWorkItems(_ context.Context, id domain.ScanID, items []domain.WorkItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.scans[id]; !ok {
		return fmt.Errorf("taskstore: scan %q not found", id)
	}
	for _, it := range items {
		m.ledger[it.ID] = &platform.WorkLedgerEntry{ID: it.ID, ScanID: id, Item: it, State: platform.ItemPending}
		m.byScan[id] = append(m.byScan[id], it.ID)
		m.notDone[id]++
	}
	return nil
}

func (m *Mem) CompareAndSetItemState(_ context.Context, wid domain.WorkItemID, expected, next platform.ItemState) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.ledger[wid]
	if !ok {
		return false, fmt.Errorf("taskstore: work item %q not found", wid)
	}
	if e.State != expected {
		return false, nil
	}
	e.State = next
	// Leaving inflight (e.g. the sweep's reclaim reset to pending) clears the
	// lease so a stale straggler cannot later match its owner.
	if next != platform.ItemInflight {
		e.LeaseOwner, e.LeaseExpiry = "", 0
	}
	return true, nil
}

// ClaimItem atomically takes a pending item OR steals an inflight item whose
// lease has expired (leaseExpiry < now), stamping the new lease (BR-L1/L4). A
// live worker's item (leaseExpiry ≥ now) is never stolen.
func (m *Mem) ClaimItem(_ context.Context, wid domain.WorkItemID, lease platform.Lease, now domain.Timestamp) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.ledger[wid]
	if !ok {
		return false, fmt.Errorf("taskstore: work item %q not found", wid)
	}
	claimable := e.State == platform.ItemPending ||
		(e.State == platform.ItemInflight && e.LeaseExpiry < now)
	if !claimable {
		return false, nil
	}
	e.State = platform.ItemInflight
	e.LeaseOwner, e.LeaseExpiry = lease.Owner, lease.Expiry
	return true, nil
}

// CountUnfinished returns |pending ∪ inflight| (O(1) via the maintained counter).
func (m *Mem) CountUnfinished(_ context.Context, id domain.ScanID) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.notDone[id], nil
}

// RefreshLease extends the lease only while still inflight and owned (BR-L2).
func (m *Mem) RefreshLease(_ context.Context, wid domain.WorkItemID, owner string, newExpiry domain.Timestamp) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.ledger[wid]
	if !ok {
		return false, nil
	}
	if e.State != platform.ItemInflight || e.LeaseOwner != owner {
		return false, nil
	}
	e.LeaseExpiry = newExpiry
	return true, nil
}

func (m *Mem) ListExpiredInflight(_ context.Context, id domain.ScanID, now domain.Timestamp) ([]domain.WorkItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []domain.WorkItem
	for _, wid := range m.byScan[id] {
		e := m.ledger[wid]
		if e != nil && e.State == platform.ItemInflight && e.LeaseExpiry < now {
			out = append(out, e.Item)
		}
	}
	return out, nil
}

// RecordOutcome finalizes only if still inflight and owned by `owner` (BR-L5), so
// a reclaimed straggler cannot overwrite the re-run worker's result.
// PurgeOutcomeEvidence drops the evidence the ledger still holds for a scan once
// its findings are stored elsewhere (see the seam doc). The mem store keeps
// everything in process memory, so there is no at-rest exposure to close here —
// but it implements the same contract so that a caller cannot behave differently
// depending on which backend it got.
func (m *Mem) PurgeOutcomeEvidence(_ context.Context, id domain.ScanID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.ledger {
		if e.ScanID != id || e.Outcome == nil {
			continue
		}
		for i := range e.Outcome.Findings {
			e.Outcome.Findings[i].Evidence = domain.Evidence{}
		}
	}
	return nil
}

func (m *Mem) RecordOutcome(_ context.Context, wid domain.WorkItemID, owner string, o domain.WorkItemOutcome) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.ledger[wid]
	if !ok {
		return false, fmt.Errorf("taskstore: work item %q not found", wid)
	}
	if e.State != platform.ItemInflight || e.LeaseOwner != owner {
		return false, nil // reclaimed/stolen — reject the stale write
	}
	oc := o
	e.Outcome = &oc
	e.State = platform.ItemDone
	e.LeaseOwner, e.LeaseExpiry = "", 0
	if m.notDone[e.ScanID] > 0 {
		m.notDone[e.ScanID]-- // item finished → leaves pending∪inflight
	}
	return true, nil
}

func (m *Mem) ListUnfinished(_ context.Context, id domain.ScanID) ([]domain.WorkItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []domain.WorkItem
	for _, wid := range m.byScan[id] {
		e := m.ledger[wid]
		if e != nil && (e.State == platform.ItemPending || e.State == platform.ItemInflight) {
			out = append(out, e.Item)
		}
	}
	return out, nil
}

func (m *Mem) AllItems(_ context.Context, id domain.ScanID) ([]domain.WorkItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.WorkItem, 0, len(m.byScan[id]))
	for _, wid := range m.byScan[id] {
		if e := m.ledger[wid]; e != nil {
			out = append(out, e.Item)
		}
	}
	return out, nil
}

func (m *Mem) ListScansByState(_ context.Context, state platform.ScanState) ([]domain.ScanID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []domain.ScanID
	for id, s := range m.scans {
		if s.State == state {
			out = append(out, id)
		}
	}
	return out, nil
}

func (m *Mem) AllOutcomes(_ context.Context, id domain.ScanID) ([]domain.WorkItemOutcome, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []domain.WorkItemOutcome
	for _, wid := range m.byScan[id] {
		if e := m.ledger[wid]; e != nil && e.Outcome != nil {
			out = append(out, *e.Outcome)
		}
	}
	return out, nil
}

func (m *Mem) SaveReport(_ context.Context, id domain.ScanID, r report.Report) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reports[id] = r
	return nil
}

func (m *Mem) LoadReport(_ context.Context, id domain.ScanID) (report.Report, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.reports[id]
	return r, ok, nil
}
