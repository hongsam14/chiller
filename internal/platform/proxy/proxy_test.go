package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// The work concurrency cap equals T even when a keep-alive tab is reserved —
// the login keep-alive tab must NOT be counted in the work pool (user constraint).
func TestFakeSession_WorkCapEqualsT_KeepAliveExcluded(t *testing.T) {
	const T = 3
	release := make(chan struct{})
	handler := func(_ context.Context, _ *http.Request) (*http.Response, error) {
		<-release // hold the tab so concurrency can build up
		return &http.Response{StatusCode: 200, Body: io.NopCloser(nil)}, nil
	}
	sess := NewFakeSession(SessionConfig{Tabs: T, ReserveKeepAlive: true}, "tok", handler)
	if sess.PoolSize() != T {
		t.Fatalf("work pool size = %d, want %d (keep-alive must not add to T)", sess.PoolSize(), T)
	}

	const callers = 10
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, "http://x/", nil)
			_, _ = sess.Do(context.Background(), req)
		}()
	}
	// Give the pool time to saturate, then verify it never exceeded T.
	deadline := time.After(2 * time.Second)
	for sess.MaxConcurrency() < T {
		select {
		case <-deadline:
			t.Fatalf("pool never reached %d concurrent (got %d)", T, sess.MaxConcurrency())
		case <-time.After(time.Millisecond):
		}
	}
	if got := sess.MaxConcurrency(); got != T {
		t.Fatalf("max concurrency = %d, want exactly %d", got, T)
	}
	close(release)
	wg.Wait()
}

// The AuthProxy injects the session and forwards to the upstream; the upstream
// sees the injected auth marker.
func TestAuthProxy_InjectsSessionAndForwards(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-Auth-Session")
		_, _ = io.WriteString(w, "hello")
	}))
	defer upstream.Close()

	// The fake session injects X-Auth-Session (via Do) then actually round-trips.
	forward := func(_ context.Context, req *http.Request) (*http.Response, error) {
		return http.DefaultTransport.RoundTrip(req)
	}
	sess := NewFakeSession(SessionConfig{Tabs: 2}, "sekret", forward)
	p := &authProxy{session: sess}

	proxySrv := httptest.NewServer(http.HandlerFunc(p.handle))
	defer proxySrv.Close()

	proxyURL, _ := url.Parse(proxySrv.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	resp, err := client.Get(upstream.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Fatalf("body = %q, want hello", body)
	}
	if gotAuth != "sekret" {
		t.Fatalf("upstream saw X-Auth-Session = %q, want sekret (session not injected)", gotAuth)
	}
}

func TestAuthProxy_ServeReturnsOnCtxCancel(t *testing.T) {
	sess := NewFakeSession(SessionConfig{Tabs: 1}, "t", func(_ context.Context, _ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(nil)}, nil
	})
	p := NewAuthProxy(sess, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Serve(ctx, "127.0.0.1:0") }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error on cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after ctx cancel")
	}
}
