// Package platform is the control plane and composition root's contract layer
// (Unit U1). It owns the orchestration entities (Scan aggregate, FSM, work
// ledger, progress) and the seam interfaces (TaskStore, WorkQueue,
// ProgressReporter, Authorizer, BrowserSession, AuthProxy, Orchestrator). It
// creates no judgement logic — the data plane (scan/findings) does that. Nothing
// in the data plane imports this package (NFR-02); the reverse is allowed.
//
// Seam design follows the Infra-swappable rule (E-6/PR-11): every store/queue/
// progress method takes context.Context first and returns error last, CAS
// returns (bool, error), and persisted entities are serializable — so the MVP
// in-memory impls can be swapped for Redis/Valkey/Mongo/OTel at Infra without
// changing any interface or orchestration caller.
package platform

import (
	"time"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/scan/ingest"
)

// ScanRequest is the raw analysis request. It is exactly the ingest request
// (URLs + Approved hosts + ScanOptions); platform reuses it rather than
// redefining the shape.
type ScanRequest = ingest.ScanRequest

// ScanState is the Scan aggregate's FSM state (E-2, US-024).
type ScanState int

const (
	// StateCreated: accepted and persisted, before fan-out.
	StateCreated ScanState = iota
	// StateRunning: WorkItems dispatched/executing (re-entered on resume).
	StateRunning
	// StateCompleted: all WorkItems terminal + coverage accounted + report built.
	StateCompleted
	// StateStopped: user-stopped; undispatched items cancelled.
	StateStopped
	// StateFailed: unrecoverable error after boot gate passed.
	StateFailed
)

var scanStateNames = [...]string{"created", "running", "completed", "stopped", "failed"}

// String renders the state for JSON/status output.
func (s ScanState) String() string {
	if s < StateCreated || int(s) >= len(scanStateNames) {
		return "unknown"
	}
	return scanStateNames[s]
}

// ParseScanState is the inverse of String — it maps the persisted string form
// back to a ScanState. It exists so the durable store (Mongo) can persist states
// as human-readable strings (Q4=A) rather than as brittle iota ordinals whose
// meaning would silently shift if the const order ever changed (F-4).
func ParseScanState(name string) (ScanState, bool) {
	for i, n := range scanStateNames {
		if n == name {
			return ScanState(i), true
		}
	}
	return 0, false
}

// Terminal reports whether the state is an end state (no further transitions
// except the fixed terminal→terminal identity). completed/stopped/failed only.
func (s ScanState) Terminal() bool {
	return s == StateCompleted || s == StateStopped || s == StateFailed
}

// ItemState is a WorkItem's ledger state (E-3). The CAS transition
// pending→inflight is how exactly one worker claims an item (PR-4, US-025).
type ItemState int

const (
	// ItemPending: enqueued, unclaimed.
	ItemPending ItemState = iota
	// ItemInflight: claimed by a worker (CAS from pending).
	ItemInflight
	// ItemDone: completed with a recorded outcome.
	ItemDone
)

var itemStateNames = [...]string{"pending", "inflight", "done"}

// String renders the item state.
func (s ItemState) String() string {
	if s < ItemPending || int(s) >= len(itemStateNames) {
		return "unknown"
	}
	return itemStateNames[s]
}

// ParseItemState is the inverse of String (see ParseScanState for the rationale).
func ParseItemState(name string) (ItemState, bool) {
	for i, n := range itemStateNames {
		if n == name {
			return ItemState(i), true
		}
	}
	return 0, false
}

// Scan is the orchestration aggregate — one user analysis request (E-1). It is
// serializable (E-6 rule 4): plain fields, no funcs/channels.
//
// The report used to be a field here. Nothing ever assigned it — reports are
// stored in their own collection by SaveReport — so every scan document carried a
// null "report" field forever. It is gone.
type Scan struct {
	ID         domain.ScanID      `bson:"_id"`
	State      ScanState          `bson:"state"`
	Request    ScanRequest        `bson:"request"`
	Frameworks []domain.Framework `bson:"frameworks"`
	CreatedAt  domain.Timestamp   `bson:"createdAt"`
	UpdatedAt  domain.Timestamp   `bson:"updatedAt"`
	// SchemaVersion marks the document layout so a future migration has an
	// explicit branch point. Current layout = 1.
	SchemaVersion int `bson:"schemaVersion"`
}

// WorkLedgerEntry tracks one fan-out WorkItem's lifecycle without extending the
// kernel WorkItem (E-3). The store keeps these per scan; ListUnfinished =
// pending ∪ inflight is the CAS-resume source of truth (US-025).
type WorkLedgerEntry struct {
	// ID mirrors Item.ID so the durable store can use it as the document _id and
	// index it for the pending→inflight CAS (the mem impl keys its map by it too).
	ID      domain.WorkItemID       `bson:"_id"`
	ScanID  domain.ScanID           `bson:"scanId"`
	Item    domain.WorkItem         `bson:"item"`
	State   ItemState               `bson:"state"`
	Outcome *domain.WorkItemOutcome `bson:"outcome"` // set when State == ItemDone
	// Lease fields track who holds an inflight item and until when, so the
	// recovery sweep can reclaim ONLY genuinely-stale items (expired lease) —
	// never one a live worker is still processing (E-L1). Cleared (""/0) whenever
	// the item leaves inflight.
	LeaseOwner  string           `bson:"leaseOwner"`
	LeaseExpiry domain.Timestamp `bson:"leaseExpiry"`
}

// Lease is the claim a worker stamps on an item when it takes it (E-L1): the
// owning worker's id and the wall-clock time after which the claim is considered
// stale and reclaimable.
type Lease struct {
	Owner  string
	Expiry domain.Timestamp
}

const (
	// LeaseTTL is how long a claim is valid without a heartbeat (Q7=A). An
	// inflight item whose lease is older than this is reclaimable by the sweep.
	LeaseTTL = 60 * time.Second
	// HeartbeatInterval is how often a busy worker refreshes its lease. It is
	// well under LeaseTTL/2 so two refreshes fit before expiry (Q2=A).
	HeartbeatInterval = 20 * time.Second
)

// ProgressSnapshot is the read model for the status endpoint (E-4, US-028).
type ProgressSnapshot struct {
	ScanID   domain.ScanID
	State    ScanState
	Total    int
	Done     int
	Failed   int
	Inflight int
	// PerChecker aggregates per-checker success/failure counts.
	PerChecker map[domain.CheckerID]CheckerCount
}

// CheckerCount is one checker's running tally in a ProgressSnapshot.
type CheckerCount struct {
	OK     int
	Failed int
}
