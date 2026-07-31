// Package store owns the findings of a scan. It is the one sink that holds a
// Finding's evidence for the long term (data-census rule O-1): the work ledger
// carries findings only while a scan is running, and a stored report references
// them by FindingKey rather than embedding a second copy.
package store

import (
	"context"
	"sync"

	"github.com/hongsam14/chiller/internal/domain"
)

// FindingsStore is the persistence seam (NFR-01, NFR-04 swappable).
//
// The contract every implementation must honour — stated rather than assumed,
// because the durable implementation cannot satisfy it by accident:
//
//   - LoadFindings returns the findings in the SAME ORDER SaveFindings was given.
//     Mongo returns documents in no particular order unless told to sort, and a
//     report is assembled from this slice, so the order is part of the contract.
//   - SaveFindings REPLACES what was there before, and is idempotent. Saving a
//     shorter list must not leave the tail of a previous save behind.
//   - A save ADDS evidence but never ERASES it. If a finding arrives with no
//     evidence and the stored one has some, the stored evidence stays.
//   - An unknown scan loads as an empty slice, not an error.
//   - Neither call retains or mutates the caller's slice.
//
// The never-erase rule is the one that is not obvious, and it exists because of a
// real data-loss bug. A scan's evidence rides in the work ledger until finalize
// purges it, so any finalize that reads the ledger AFTER that purge — a re-trigger,
// a second finalizer in another pod — produces the same findings with EMPTY
// evidence. A store that faithfully replaced would then overwrite the sealed
// evidence with nothing, and this store is the last copy. "No evidence" is never
// new information about a finding that already has some.
type FindingsStore interface {
	SaveFindings(ctx context.Context, scanID domain.ScanID, findings []domain.Finding) error
	LoadFindings(ctx context.Context, scanID domain.ScanID) ([]domain.Finding, error)
}

// InMemory is a non-durable FindingsStore for tests and local development.
// It does not encrypt evidence: its contents never leave the process. The Mongo
// implementation, whose contents do, seals evidence before writing it.
type InMemory struct {
	mu sync.RWMutex
	m  map[domain.ScanID][]domain.Finding
}

var _ FindingsStore = (*InMemory)(nil)

// NewInMemory builds an empty in-memory store.
func NewInMemory() *InMemory { return &InMemory{m: map[domain.ScanID][]domain.Finding{}} }

func (s *InMemory) SaveFindings(_ context.Context, scanID domain.ScanID, findings []domain.Finding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[scanID] = keepExistingEvidence(cloneFindings(findings), s.m[scanID])
	return nil
}

// keepExistingEvidence applies the never-erase rule (see the FindingsStore doc): an
// incoming finding with no evidence does not wipe out the evidence already stored
// for the same finding.
func keepExistingEvidence(incoming, stored []domain.Finding) []domain.Finding {
	if len(stored) == 0 {
		return incoming
	}
	have := make(map[domain.FindingKey]domain.Evidence, len(stored))
	for _, f := range stored {
		if !f.Evidence.Empty() {
			have[f.Key()] = f.Evidence
		}
	}
	for i := range incoming {
		if !incoming[i].Evidence.Empty() {
			continue
		}
		if ev, ok := have[incoming[i].Key()]; ok {
			incoming[i].Evidence = ev
		}
	}
	return incoming
}

func (s *InMemory) LoadFindings(_ context.Context, scanID domain.ScanID) ([]domain.Finding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneFindings(s.m[scanID]), nil
}

// cloneFindings deep-copies findings so the store and its callers never share
// mutable state. Copying the slice alone is not enough: a Finding's CWEs and
// evidence snippets are themselves slices, so a shallow copy would let a caller
// mutate what is "stored" — an aliasing bug the durable implementations cannot
// have (they serialize), which is exactly why the in-memory one has to be held to
// the same contract.
func cloneFindings(in []domain.Finding) []domain.Finding {
	out := make([]domain.Finding, len(in))
	for i, f := range in {
		f.CWEs = append([]domain.CWE(nil), f.CWEs...)
		f.Evidence = cloneEvidence(f.Evidence)
		out[i] = f
	}
	return out
}

// cloneEvidence deep-copies the structured evidence so no slice/pointer is shared
// with the caller (the same aliasing contract the durable stores get by serializing).
func cloneEvidence(e domain.Evidence) domain.Evidence {
	out := domain.Evidence{}
	if len(e.Items) > 0 {
		out.Items = append([]domain.EvidenceItem(nil), e.Items...)
	}
	if e.Request != nil {
		r := *e.Request
		r.Headers = append([]domain.Header(nil), e.Request.Headers...)
		out.Request = &r
	}
	if e.Response != nil {
		r := *e.Response
		r.Headers = append([]domain.Header(nil), e.Response.Headers...)
		out.Response = &r
	}
	return out
}
