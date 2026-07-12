package proxy

import (
	"context"
	"io"
	"net/http"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/platform"
	"github.com/enki-offen/chiller/internal/scan/safety"
)

// authProxy is the local forward proxy workers egress through in financial mode
// (PR-12, Q3=B). It receives a worker's outbound HTTP request (absolute-form, as
// an HTTP proxy does), re-issues it via the single BrowserSession — which injects
// the authenticated session — and streams the response back. Because every
// worker points at this one proxy, and this proxy holds the one BrowserSession,
// the single-session invariant holds as workers scale horizontally.
//
// MVP scope: plain-HTTP absolute-form forwarding. HTTPS via CONNECT + a bundled
// MITM CA is deferred to Infra (honest scope) — the seam and the session path
// are real now.
type authProxy struct {
	session platform.BrowserSession
	lim     safety.RateLimiter
	server  *http.Server
}

// NewAuthProxy builds an AuthProxy over a BrowserSession. lim enforces the
// per-host rate budget (SR-4) on the proxied egress; pass nil to disable.
func NewAuthProxy(session platform.BrowserSession, lim safety.RateLimiter) platform.AuthProxy {
	return &authProxy{session: session, lim: lim}
}

// NewHandler returns the forwarding http.Handler an AuthProxy serves — the same
// handler Serve mounts, exported so it can be embedded (e.g. behind a test
// httptest proxy) without opening a socket.
func NewHandler(session platform.BrowserSession, lim safety.RateLimiter) http.Handler {
	p := &authProxy{session: session, lim: lim}
	return http.HandlerFunc(p.handle)
}

var _ platform.AuthProxy = (*authProxy)(nil)

func (p *authProxy) Serve(ctx context.Context, addr string) error {
	p.server = &http.Server{Addr: addr, Handler: http.HandlerFunc(p.handle)}
	go func() {
		<-ctx.Done()
		_ = p.server.Close() // unblock Serve when the scan/process shuts down
	}()
	if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (p *authProxy) handle(w http.ResponseWriter, r *http.Request) {
	// A forward-proxy request carries an absolute URL. Reject anything else
	// (e.g. a direct request) fail-closed.
	if !r.URL.IsAbs() || r.URL.Host == "" {
		http.Error(w, "proxy: expected absolute-form request", http.StatusBadRequest)
		return
	}
	// Per-host rate budget on the authenticated egress (SR-4).
	if p.lim != nil {
		release, err := p.lim.Acquire(hostOf(r))
		if err != nil {
			http.Error(w, "proxy: rate", http.StatusServiceUnavailable)
			return
		}
		defer release()
	}

	out, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, "proxy: build request", http.StatusBadGateway)
		return
	}
	copyHeaders(out.Header, r.Header)

	resp, err := p.session.Do(r.Context(), out) // session injection happens here
	if err != nil {
		http.Error(w, "proxy: upstream", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func hostOf(r *http.Request) string {
	t := domain.Target{Host: r.URL.Hostname()}
	return t.Host
}

func copyHeaders(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}
