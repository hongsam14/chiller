package fetch

import "sync/atomic"

// CacheStats counts what the response cache actually did. It lives here, in the
// package that does the caching, rather than in platform: the data plane must not
// depend on the platform layer (NFR-02), so the composition root creates one of
// these, hands it to the Fetcher, and lets the introspection endpoint read it.
//
// These three numbers are the only way to tell three very different situations
// apart, all of which look like "the scan completed" from the outside:
//
//   - hits climbing → the single-fetch reuse (FR-02) is working
//   - all misses    → workers are pointed at different caches, or the cache is empty;
//     the target is being fetched once per checker family instead of once
//   - errors        → the backend is down and every lookup is failing open
type CacheStats struct {
	Hits   atomic.Int64
	Misses atomic.Int64
	Errors atomic.Int64
}

// StatsSnapshot is a CacheStats reading, safe to serialize.
type StatsSnapshot struct {
	Hits   int64 `json:"cacheHits"`
	Misses int64 `json:"cacheMisses"`
	Errors int64 `json:"cacheErrors"`
}

// Snapshot reads the counters. A nil CacheStats reads as zero, so an unwired
// Fetcher (tests, local runs) needs no special-casing.
func (s *CacheStats) Snapshot() StatsSnapshot {
	if s == nil {
		return StatsSnapshot{}
	}
	return StatsSnapshot{Hits: s.Hits.Load(), Misses: s.Misses.Load(), Errors: s.Errors.Load()}
}

func (s *CacheStats) hit() {
	if s != nil {
		s.Hits.Add(1)
	}
}

func (s *CacheStats) miss() {
	if s != nil {
		s.Misses.Add(1)
	}
}

func (s *CacheStats) err() {
	if s != nil {
		s.Errors.Add(1)
	}
}
