package fetch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/scan/safety"
)

// WithProxy routes the Fetcher's egress through the given proxy (PR-12c): the
// GET for a target must be delivered to the proxy, not the origin directly.
func TestFetcher_WithProxy_EgressesThroughProxy(t *testing.T) {
	var proxied string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied = r.URL.String() // absolute-form target the proxy was asked to fetch
		_, _ = io.WriteString(w, "via-proxy")
	}))
	defer proxy.Close()

	gate := safety.NewApprovalGate([]string{"target.example"}, nil)
	lim := safety.NewRateLimiter(0, 0) // disabled
	cache := NewInMemoryCache()
	f := NewFetcher(gate, lim, cache, &http.Client{}, WithProxy(proxy.URL))

	target := domain.Target{URL: "http://target.example/x", Host: "target.example", Scheme: "http"}
	he, err := f.Fetch(context.Background(), "scan-1", target)
	if err != nil {
		t.Fatal(err)
	}
	if string(he.Body) != "via-proxy" {
		t.Fatalf("body = %q, want via-proxy (did not go through proxy)", he.Body)
	}
	if pu, _ := url.Parse(proxied); pu == nil || pu.Host != "target.example" {
		t.Fatalf("proxy fetched %q, want the target through the proxy", proxied)
	}
}
