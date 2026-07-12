package fetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/scan/safety"
)

// flakyRT fails the first `fails` round trips with a recoverable network error,
// then delegates to the real transport. n counts total round trips.
type flakyRT struct {
	fails int
	mu    sync.Mutex
	n     int
}

func (r *flakyRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.n++
	cur := r.n
	r.mu.Unlock()
	if cur <= r.fails {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	}
	return http.DefaultTransport.RoundTrip(req)
}

func (r *flakyRT) count() int { r.mu.Lock(); defer r.mu.Unlock(); return r.n }

// countLim counts Acquire calls so tests can prove each attempt spends a token.
type countLim struct {
	mu sync.Mutex
	n  int
}

func (c *countLim) Acquire(string) (func(), error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return func() {}, nil
}
func (c *countLim) count() int { c.mu.Lock(); defer c.mu.Unlock(); return c.n }

func approveHost(host string) safety.ApprovalGate {
	return safety.NewApprovalGate([]string{host}, nil)
}

// A transient transport error is retried and the fetch ultimately succeeds; each
// attempt (initial + retries) spends exactly one per-host rate token (SR-4 not
// bypassed by retries).
func TestFetcher_RetriesTransientThenSucceeds_ChargesBudgetPerAttempt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	rt := &flakyRT{fails: 2} // fail twice, succeed on the 3rd
	lim := &countLim{}
	f := NewFetcher(approveHost(hostOfURL(t, srv.URL)), lim, NewInMemoryCache(),
		&http.Client{Transport: rt}) // default retries = 2

	target := domain.Target{URL: srv.URL + "/", Host: hostOfURL(t, srv.URL), Scheme: "http"}
	he, err := f.Fetch(context.Background(), "s", target)
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if string(he.Body) != "ok" {
		t.Fatalf("body = %q, want ok", he.Body)
	}
	if got := rt.count(); got != 3 {
		t.Fatalf("round trips = %d, want 3 (1 + 2 retries)", got)
	}
	// 1 pre-request Acquire + 1 per retry decision = 3 tokens for 3 attempts.
	if got := lim.count(); got != 3 {
		t.Fatalf("rate tokens spent = %d, want 3 (one per attempt — retries must charge the budget)", got)
	}
}

// WithRetries(0) disables retries: a transient failure returns immediately.
func TestFetcher_WithRetriesZero_NoRetry(t *testing.T) {
	rt := &flakyRT{fails: 5}
	lim := &countLim{}
	f := NewFetcher(approveHost("target.example"), lim, NewInMemoryCache(),
		&http.Client{Transport: rt}, WithRetries(0))

	target := domain.Target{URL: "http://target.example/", Host: "target.example", Scheme: "http"}
	if _, err := f.Fetch(context.Background(), "s", target); err == nil {
		t.Fatal("expected error with retries disabled")
	}
	if got := rt.count(); got != 1 {
		t.Fatalf("round trips = %d, want 1 (no retry)", got)
	}
}

// A 5xx is a valid observed response, not a transport error: it is NOT retried
// (passive scanner must not hammer a struggling host), and is returned as-is.
func TestFetcher_5xx_NotRetried(t *testing.T) {
	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	host := hostOfURL(t, srv.URL)
	f := NewFetcher(approveHost(host), safety.NewRateLimiter(0, 0), NewInMemoryCache(), &http.Client{})
	target := domain.Target{URL: srv.URL + "/", Host: host, Scheme: "http"}
	he, err := f.Fetch(context.Background(), "s", target)
	if err != nil {
		t.Fatalf("5xx should be returned as a response, not an error: %v", err)
	}
	if he.Status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", he.Status)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("server hit %d times, want 1 (5xx must not be retried)", hits)
	}
}

func hostOfURL(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Hostname()
}
