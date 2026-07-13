// Package fetch performs the single-fetch-reuse acquisition + the JS discovery
// seam (US-002, SR-2/5). The ResponseCache is transient, scan-scoped: it holds
// whole response bodies, headers and cookies, so it is wiped when a scan ends
// (FR-12/NFR-16) and the durable implementation encrypts what it stores.
package fetch

import (
	"context"
	"sync"

	"github.com/hongsam14/chiller/internal/domain"
)

// ResponseCache holds fetched responses (HAR) for cross-family reuse within a
// scan (FR-02). It is working state, wiped post-scan (FR-12/NFR-16) — never the
// findings store.
//
// Get returns (entry, found, error) rather than the (entry, found) you might
// expect, because a network-backed cache has a third outcome: "I don't know".
// Folding a backend failure into a miss would make a permanently broken cache
// indistinguishable from a cold one — the scan would still be correct, just
// silently refetching every URL and hammering the target. The caller still treats
// an error as a miss (fail-open), but it can only count what it can see.
type ResponseCache interface {
	Put(ctx context.Context, scanID domain.ScanID, url string, e *domain.HAREntry) error
	Get(ctx context.Context, scanID domain.ScanID, url string) (*domain.HAREntry, bool, error)
	WipeScan(ctx context.Context, scanID domain.ScanID) error
}

// InMemory is a non-durable ResponseCache. It does not encrypt: its contents never
// leave the process (the Valkey implementation, whose do, seals them).
type InMemory struct {
	mu sync.RWMutex
	m  map[domain.ScanID]map[string]*domain.HAREntry
}

var _ ResponseCache = (*InMemory)(nil)

// NewInMemoryCache builds an empty cache.
func NewInMemoryCache() *InMemory {
	return &InMemory{m: map[domain.ScanID]map[string]*domain.HAREntry{}}
}

func (c *InMemory) Put(_ context.Context, scanID domain.ScanID, url string, e *domain.HAREntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m[scanID] == nil {
		c.m[scanID] = map[string]*domain.HAREntry{}
	}
	c.m[scanID][url] = e
	return nil
}

func (c *InMemory) Get(_ context.Context, scanID domain.ScanID, url string) (*domain.HAREntry, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.m[scanID][url]
	return e, ok, nil
}

func (c *InMemory) WipeScan(_ context.Context, scanID domain.ScanID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, scanID)
	return nil
}
