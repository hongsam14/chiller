package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/scan/safety"
)

// brokenCache is a ResponseCache whose backend is down: every call fails.
type brokenCache struct{ gets, puts int }

func (c *brokenCache) Put(context.Context, domain.ScanID, string, *domain.HAREntry) error {
	c.puts++
	return errors.New("valkey: connection refused")
}

func (c *brokenCache) Get(context.Context, domain.ScanID, string) (*domain.HAREntry, bool, error) {
	c.gets++
	return nil, false, errors.New("valkey: connection refused")
}

func (c *brokenCache) WipeScan(context.Context, domain.ScanID) error {
	return errors.New("valkey: connection refused")
}

// TestFetcher_CacheFailsOpen: when the cache backend is down, a scan must still
// run. The cache is an optimization — losing it should cost speed, not results.
func TestFetcher_CacheFailsOpen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	defer srv.Close()

	tgt := target(t, srv.URL)
	cache := &brokenCache{}
	stats := &CacheStats{}
	f := NewFetcher(safety.NewApprovalGate([]string{tgt.Host}, nil), safety.NewRateLimiter(0, 0),
		cache, srv.Client(), WithCacheStats(stats))

	for i := 0; i < 2; i++ {
		e, err := f.Fetch(context.Background(), "scan-1", tgt)
		if err != nil {
			t.Fatalf("fetch %d failed because the CACHE was down: %v", i, err)
		}
		if string(e.Body) != "<html>ok</html>" {
			t.Fatalf("fetch %d returned the wrong body", i)
		}
	}

	// Both lookups and both writes were attempted and both failed — and the failure
	// was COUNTED. A broken cache that silently reported misses would look exactly
	// like a cold one, and nobody would ever find out the reuse had stopped working.
	if cache.gets != 2 {
		t.Errorf("attempted %d cache reads, want 2", cache.gets)
	}
	if got := stats.Errors.Load(); got != 4 { // 2 failed gets + 2 failed puts
		t.Errorf("counted %d cache errors, want 4 — a fail-open cache must not fail silently", got)
	}
	if stats.Hits.Load() != 0 {
		t.Error("a failing cache must never be counted as a hit")
	}
}

// TestFetcher_CacheFailureStillChargesRateBudget pins the reason fail-open is safe.
// Refetching because the cache is down means extra traffic to the target — which
// is only acceptable because a refetch goes through the same per-host rate budget
// as a first fetch. If it bypassed the budget, a cache outage would turn into a
// burst of requests at the scan target.
func TestFetcher_CacheFailureStillChargesRateBudget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	tgt := target(t, srv.URL)
	lim := &countingLimiter{inner: safety.NewRateLimiter(0, 0)}
	f := NewFetcher(safety.NewApprovalGate([]string{tgt.Host}, nil), lim, &brokenCache{}, srv.Client())

	for i := 0; i < 3; i++ {
		if _, err := f.Fetch(context.Background(), "scan-1", tgt); err != nil {
			t.Fatal(err)
		}
	}
	if lim.acquired < 3 {
		t.Errorf("the rate budget was charged %d times for 3 refetches — a cache outage must not "+
			"become a way around the per-host limit (SR-4)", lim.acquired)
	}
}

// TestFetcher_CacheHitSkipsTheNetwork is the flip side: when the cache works, the
// URL is fetched exactly once no matter how many checkers ask for it (FR-02).
func TestFetcher_CacheHitSkipsTheNetwork(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(200)
	}))
	defer srv.Close()

	tgt := target(t, srv.URL)
	stats := &CacheStats{}
	f := NewFetcher(safety.NewApprovalGate([]string{tgt.Host}, nil), safety.NewRateLimiter(0, 0),
		NewInMemoryCache(), srv.Client(), WithCacheStats(stats))

	for i := 0; i < 5; i++ {
		if _, err := f.Fetch(context.Background(), "scan-1", tgt); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 1 {
		t.Errorf("the origin was hit %d times for 5 fetches of the same URL, want 1 (FR-02)", hits)
	}
	if stats.Hits.Load() != 4 || stats.Misses.Load() != 1 {
		t.Errorf("stats: %d hits / %d misses, want 4/1", stats.Hits.Load(), stats.Misses.Load())
	}
}

type countingLimiter struct {
	inner    safety.RateLimiter
	acquired int
}

func (l *countingLimiter) Acquire(host string) (func(), error) {
	l.acquired++
	return l.inner.Acquire(host)
}

// TestFetcher_DoesNotCacheAfterTheScanWasCancelled: StopScan cancels the scan
// context and then wipes the cache, but a fetch already past its ctx checks runs to
// completion. A Put from that in-flight fetch would land AFTER the wipe, putting the
// response body back into the cache the wipe just cleared — and there it would stay
// until the TTL, which is the FR-12 leak the wipe exists to close.
func TestFetcher_DoesNotCacheAfterTheScanWasCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("<html>secret</html>"))
	}))
	defer srv.Close()

	tgt := target(t, srv.URL)
	cache := NewInMemoryCache()
	f := NewFetcher(safety.NewApprovalGate([]string{tgt.Host}, nil), safety.NewRateLimiter(0, 0),
		cache, srv.Client())

	// A context that is already done, standing in for "StopScan cancelled us while
	// this fetch was in flight".
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// The fetch itself may or may not fail depending on where the cancellation lands;
	// what must NOT happen is a cache write.
	_, _ = f.Fetch(ctx, "scan-stopped", tgt)

	if _, hit, _ := cache.Get(context.Background(), "scan-stopped", tgt.URL); hit {
		t.Error("an in-flight fetch cached a response for a scan that was already stopped — " +
			"it would survive the wipe that follows the stop")
	}
}
