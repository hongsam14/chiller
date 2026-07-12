package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/scan/safety"
)

func target(t *testing.T, raw string) domain.Target {
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return domain.Target{URL: raw, Host: u.Hostname(), Scheme: u.Scheme}
}

func TestFetcher_FetchAndCacheReuse(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "1"}) // no Secure/HttpOnly
		w.Write([]byte("<html><script src=\"/app.js\"></script></html>"))
	}))
	defer srv.Close()

	tgt := target(t, srv.URL+"/")
	gate := safety.NewApprovalGate([]string{tgt.Host}, nil)
	cache := NewInMemoryCache()
	f := NewFetcher(gate, safety.NewRateLimiter(0, 0), cache, srv.Client())

	e, err := f.Fetch(context.Background(), "scan-1", tgt)
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != 200 || len(e.Cookies) != 1 || e.Cookies[0].Secure {
		t.Errorf("HAR wrong: %+v", e)
	}
	// second fetch of the same URL is served from cache (FR-02: one GET).
	if _, err := f.Fetch(context.Background(), "scan-1", tgt); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Errorf("expected 1 out-going GET (cache reuse), got %d", hits)
	}
}

func TestFetcher_FailClosed(t *testing.T) {
	// unapproved host → rejected before any request.
	gate := safety.NewApprovalGate([]string{"approved.test"}, nil)
	f := NewFetcher(gate, safety.NewRateLimiter(0, 0), NewInMemoryCache(), http.DefaultClient)
	if _, err := f.Fetch(context.Background(), "s", domain.Target{URL: "https://evil.test/", Host: "evil.test", Scheme: "https"}); err == nil {
		t.Error("unapproved target must be rejected")
	}
}

// H1 regression: a redirect to an unapproved host must be blocked — the gate is
// re-checked on every hop, not just the initial URL. CheckRedirect runs BEFORE
// the redirect request is issued, so an unresolvable unapproved host proves the
// hop is rejected rather than attempted.
func TestFetcher_RedirectToUnapprovedHost_Blocked(t *testing.T) {
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://blocked.invalid/loot", http.StatusFound)
	}))
	defer src.Close()

	tgt := target(t, src.URL+"/")
	gate := safety.NewApprovalGate([]string{tgt.Host}, nil) // only the source host approved
	f := NewFetcher(gate, safety.NewRateLimiter(0, 0), NewInMemoryCache(), src.Client())
	_, err := f.Fetch(context.Background(), "s", tgt)
	if err == nil {
		t.Fatal("redirect to an unapproved host must fail the fetch")
	}
	// The error must come from the gate's fail-closed rejection, not from trying
	// (and failing) to dial the unapproved host — i.e. we never went outbound.
	if !errors.Is(err, safety.ErrNotApproved) {
		t.Errorf("expected fail-closed ErrNotApproved on redirect hop, got %v", err)
	}
}

// H5 regression: per-page JS discovery is capped so one page cannot fan out into
// an unbounded burst of fetches.
func TestStaticDiscoverer_CapsFanout(t *testing.T) {
	tgt := target(t, "https://a.test/page")
	gate := safety.NewApprovalGate([]string{"a.test"}, nil)
	d := NewStaticDiscoverer(gate)
	var body []byte
	for i := 0; i < maxDiscoveredJS+50; i++ {
		body = append(body, []byte("<script src=\"/a"+itoa(i)+".js\"></script>")...)
	}
	got := d.Discover(tgt, &domain.HAREntry{Body: body})
	if len(got) > maxDiscoveredJS {
		t.Errorf("discovery not capped: got %d, want <= %d", len(got), maxDiscoveredJS)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestStaticDiscoverer_ApprovedOnly(t *testing.T) {
	tgt := target(t, "https://a.test/page")
	gate := safety.NewApprovalGate([]string{"a.test"}, nil) // only a.test approved
	d := NewStaticDiscoverer(gate)
	html := &domain.HAREntry{Body: []byte(
		`<script src="/app.js"></script><script src="https://cdn.evil.test/x.js"></script>`)}
	got := d.Discover(tgt, html)
	if len(got) != 1 || got[0].Host != "a.test" {
		t.Errorf("should discover only the approved same-origin JS, got %+v", got)
	}
}
