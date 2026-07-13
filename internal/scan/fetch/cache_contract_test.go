package fetch_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/scan/fetch"
)

// runResponseCacheContract is the implementation-independent behavioral suite for
// fetch.ResponseCache. InMemory (below) and the durable Valkey impl (in the
// integration test) run the EXACT same suite, so swapping the backend must
// reproduce identical observable behavior.
func runResponseCacheContract(t *testing.T, newCache func() fetch.ResponseCache) {
	t.Helper()
	t.Run("RoundTrip", func(t *testing.T) { testCacheRoundTrip(t, newCache()) })
	t.Run("MissIsNotAnError", func(t *testing.T) { testCacheMiss(t, newCache()) })
	t.Run("ScansAreIsolated", func(t *testing.T) { testCacheScanIsolation(t, newCache()) })
	t.Run("WipeLeavesNothing", func(t *testing.T) { testCacheWipe(t, newCache()) })
	t.Run("WipeIsIdempotent", func(t *testing.T) { testCacheWipeIdempotent(t, newCache()) })
	t.Run("PBT_StateModel", func(t *testing.T) { testCachePBT(t, newCache()) })
}

func TestInMemoryCache_Contract(t *testing.T) {
	runResponseCacheContract(t, func() fetch.ResponseCache { return fetch.NewInMemoryCache() })
}

func entry(url string, body string) *domain.HAREntry {
	return &domain.HAREntry{
		URL:     url,
		Scheme:  "https",
		Status:  200,
		Headers: []domain.Header{{Name: "Server", Value: "nginx"}},
		Cookies: []domain.Cookie{{Name: "SESSION", Value: "s3cret", Secure: true}},
		Body:    []byte(body),
	}
}

func testCacheRoundTrip(t *testing.T, c fetch.ResponseCache) {
	ctx := context.Background()
	want := entry("https://x.test/a", "<html>hello</html>")
	if err := c.Put(ctx, "scan-1", want.URL, want); err != nil {
		t.Fatal(err)
	}
	got, hit, err := c.Get(ctx, "scan-1", want.URL)
	if err != nil || !hit {
		t.Fatalf("Get: hit=%v err=%v", hit, err)
	}
	if string(got.Body) != string(want.Body) || got.Status != want.Status {
		t.Errorf("round-trip lost data: %+v", got)
	}
	// The body has to survive intact — the observation signatures regex over it.
	if len(got.Cookies) != 1 || got.Cookies[0].Value != "s3cret" || !got.Cookies[0].Secure {
		t.Errorf("cookies did not round-trip: %+v", got.Cookies)
	}
	if len(got.Headers) != 1 || got.Headers[0].Name != "Server" {
		t.Errorf("headers did not round-trip: %+v", got.Headers)
	}
}

func testCacheMiss(t *testing.T, c fetch.ResponseCache) {
	got, hit, err := c.Get(context.Background(), "scan-1", "https://never.test/")
	if err != nil {
		t.Fatalf("a miss must not be an error: %v", err)
	}
	if hit || got != nil {
		t.Error("expected a clean miss")
	}
}

func testCacheScanIsolation(t *testing.T, c fetch.ResponseCache) {
	ctx := context.Background()
	url := "https://x.test/same"
	if err := c.Put(ctx, "scan-a", url, entry(url, "A")); err != nil {
		t.Fatal(err)
	}
	if err := c.Put(ctx, "scan-b", url, entry(url, "B")); err != nil {
		t.Fatal(err)
	}
	a, _, _ := c.Get(ctx, "scan-a", url)
	b, _, _ := c.Get(ctx, "scan-b", url)
	if a == nil || b == nil || string(a.Body) != "A" || string(b.Body) != "B" {
		t.Error("the same URL in two scans must not collide")
	}
}

// testCacheWipe is the one that matters for FR-12/NFR-16: after a scan ends, none
// of its response bodies may remain.
func testCacheWipe(t *testing.T, c fetch.ResponseCache) {
	ctx := context.Background()
	urls := []string{"https://x.test/1", "https://x.test/2", "https://x.test/3"}
	for _, u := range urls {
		if err := c.Put(ctx, "scan-wipe", u, entry(u, "body")); err != nil {
			t.Fatal(err)
		}
	}
	// A second scan's entries must survive the first scan's wipe.
	if err := c.Put(ctx, "scan-other", urls[0], entry(urls[0], "other")); err != nil {
		t.Fatal(err)
	}

	if err := c.WipeScan(ctx, "scan-wipe"); err != nil {
		t.Fatal(err)
	}
	for _, u := range urls {
		if _, hit, err := c.Get(ctx, "scan-wipe", u); err != nil || hit {
			t.Errorf("%s survived the wipe (hit=%v err=%v)", u, hit, err)
		}
	}
	if _, hit, _ := c.Get(ctx, "scan-other", urls[0]); !hit {
		t.Error("wiping one scan must not wipe another")
	}
}

func testCacheWipeIdempotent(t *testing.T, c fetch.ResponseCache) {
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := c.WipeScan(ctx, "never-existed"); err != nil {
			t.Fatalf("wiping an unknown scan must be a no-op, got %v", err)
		}
	}
}

// testCachePBT is the stateful property (PBT-06): a random sequence of puts, gets
// and wipes must leave the cache agreeing with a plain map model at every step —
// and after a wipe, the model says every one of that scan's URLs is a miss.
func testCachePBT(t *testing.T, c fetch.ResponseCache) {
	ctx := context.Background()
	rng := rand.New(rand.NewSource(20260713))
	model := map[domain.ScanID]map[string]string{} // scan → url → body

	scans := []domain.ScanID{"s1", "s2"}
	urls := []string{"https://x.test/a", "https://x.test/b", "https://x.test/c"}

	for step := 0; step < 400; step++ {
		s := scans[rng.Intn(len(scans))]
		u := urls[rng.Intn(len(urls))]

		switch rng.Intn(10) {
		case 0: // wipe (rare, like a real scan ending)
			if err := c.WipeScan(ctx, s); err != nil {
				t.Fatalf("seed 20260713 step %d: wipe: %v", step, err)
			}
			delete(model, s)
		case 1, 2, 3: // put
			body := fmt.Sprintf("body-%d", step)
			if err := c.Put(ctx, s, u, entry(u, body)); err != nil {
				t.Fatalf("seed 20260713 step %d: put: %v", step, err)
			}
			if model[s] == nil {
				model[s] = map[string]string{}
			}
			model[s][u] = body
		default: // get
			got, hit, err := c.Get(ctx, s, u)
			if err != nil {
				t.Fatalf("seed 20260713 step %d: get: %v", step, err)
			}
			want, wantHit := model[s][u]
			if hit != wantHit {
				t.Fatalf("seed 20260713 step %d: hit=%v, model says %v", step, hit, wantHit)
			}
			if hit && string(got.Body) != want {
				t.Fatalf("seed 20260713 step %d: body %q, model says %q", step, got.Body, want)
			}
		}
	}
}
