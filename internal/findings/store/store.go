// Package store persists findings with bounded, encrypted-at-rest evidence
// (US-022, FR-12/SEC-01/03). The interface is here; the Mongo impl is deferred
// to Infrastructure/Build. An in-memory impl backs tests and local runs.
package store

import (
	"sync"

	"github.com/enki-offen/chiller/internal/domain"
)

// FindingsStore is the persistence seam (NFR-01, NFR-04 swappable).
type FindingsStore interface {
	SaveFindings(scanID domain.ScanID, findings []domain.Finding) error
	LoadFindings(scanID domain.ScanID) ([]domain.Finding, error)
}

// InMemory is a non-durable FindingsStore for tests and local development.
// A durable (Mongo) implementation is added at Infrastructure/Build.
type InMemory struct {
	mu sync.RWMutex
	m  map[domain.ScanID][]domain.Finding
}

// NewInMemory builds an empty in-memory store.
func NewInMemory() *InMemory { return &InMemory{m: map[domain.ScanID][]domain.Finding{}} }

func (s *InMemory) SaveFindings(scanID domain.ScanID, findings []domain.Finding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]domain.Finding, len(findings))
	copy(cp, findings)
	s.m[scanID] = cp
	return nil
}

func (s *InMemory) LoadFindings(scanID domain.ScanID) ([]domain.Finding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]domain.Finding, len(s.m[scanID]))
	copy(cp, s.m[scanID])
	return cp, nil
}
