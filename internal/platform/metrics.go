package platform

import "sync/atomic"

// Metrics are the persistence counters this build exposes (Q12=A). They are plain
// atomics rather than an OTel instrument because the OTel exporter is the next
// increment's job; when it lands it reads these, so the emission points do not
// have to move again.
//
// The cache hit/miss/error counters are NOT here: they belong to the scan unit
// (fetch.CacheStats), which must not depend on this package (NFR-02). What is here
// is what the orchestrator itself does — wiping the cache and purging the ledger
// when a scan ends. Both fail non-fatally, so without a counter a persistent
// failure of either would be invisible: the scan still completes, it just quietly
// leaves response bodies and evidence behind.
type Metrics struct {
	CacheWipes        atomic.Int64
	CacheWipeErrors   atomic.Int64
	LedgerPurgeErrors atomic.Int64
}

// MetricsSnapshot is a Metrics reading, safe to serialize.
type MetricsSnapshot struct {
	CacheWipes        int64 `json:"cacheWipes"`
	CacheWipeErrors   int64 `json:"cacheWipeErrors"`
	LedgerPurgeErrors int64 `json:"ledgerPurgeErrors"`
}

// Snapshot reads the counters.
func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		CacheWipes:        m.CacheWipes.Load(),
		CacheWipeErrors:   m.CacheWipeErrors.Load(),
		LedgerPurgeErrors: m.LedgerPurgeErrors.Load(),
	}
}
