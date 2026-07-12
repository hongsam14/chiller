// Package fetch performs the single-fetch-reuse acquisition + the JS discovery
// seam (US-002, SR-2/5). The ResponseCache is transient, scan-scoped; the Valkey
// impl (NFR-16) is deferred to Infrastructure — an in-memory impl backs tests
// and local runs.
package fetch

import (
	"sync"

	"github.com/enki-offen/chiller/internal/domain"
)

// ResponseCache holds fetched responses (HAR) for cross-family reuse within a
// scan (FR-02). It is working state, wiped post-scan (FR-12/NFR-16) — never the
// findings store.
type ResponseCache interface {
	Put(scanID domain.ScanID, url string, e *domain.HAREntry) error
	Get(scanID domain.ScanID, url string) (*domain.HAREntry, bool)
	WipeScan(scanID domain.ScanID) error
}

// InMemory is a non-durable ResponseCache (Valkey impl at Infra).
type InMemory struct {
	mu sync.RWMutex
	m  map[domain.ScanID]map[string]*domain.HAREntry
}

// NewInMemoryCache builds an empty cache.
func NewInMemoryCache() *InMemory {
	return &InMemory{m: map[domain.ScanID]map[string]*domain.HAREntry{}}
}

func (c *InMemory) Put(scanID domain.ScanID, url string, e *domain.HAREntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m[scanID] == nil {
		c.m[scanID] = map[string]*domain.HAREntry{}
	}
	c.m[scanID][url] = e
	return nil
}

func (c *InMemory) Get(scanID domain.ScanID, url string) (*domain.HAREntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.m[scanID][url]
	return e, ok
}

func (c *InMemory) WipeScan(scanID domain.ScanID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, scanID)
	return nil
}
