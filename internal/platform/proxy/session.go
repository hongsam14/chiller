// Package proxy implements the financial-mode authenticated transport (PR-12,
// Q1=B/Q3=B/Q4=A): a single BrowserSession that owns THE one authenticated
// browser context, and an AuthProxy that workers egress through. Concurrency is
// bounded by a WORK tab pool of size T; a login keep-alive tab is reserved
// SEPARATELY and excluded from T (a teammate's future login-agent package drives
// it via the LoginKeeper seam).
package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/enki-offen/chiller/internal/platform"
	"github.com/playwright-community/playwright-go"
)

// SessionConfig configures a BrowserSession.
type SessionConfig struct {
	// StorageStatePath is the pre-provided authenticated Playwright storageState
	// (Q2=A): cookies/localStorage of an already-logged-in session.
	StorageStatePath string
	// Tabs is the WORK tab pool size T (Q4=A default 6): the maximum number of
	// concurrent requests the session serves. The login keep-alive tab is NOT
	// part of this pool (see ReserveKeepAlive). <=0 ⇒ 1.
	Tabs int
	// ReserveKeepAlive reserves one ADDITIONAL tab, outside the work pool, for a
	// LoginKeeper to keep the session alive. Total open tabs = Tabs + 1 when set;
	// the work concurrency cap stays exactly Tabs.
	ReserveKeepAlive bool
}

// LoginKeeper is the seam for a teammate's future login-agent package. It keeps
// the authenticated session alive on its OWN dedicated tab — reserved outside
// the work pool of T, so it never reduces scan concurrency. The MVP only
// reserves the slot; the teammate's package implements Keep and is injected via
// WithLoginKeeper.
type LoginKeeper interface {
	// Keep runs for the session's lifetime, refreshing the session on its
	// dedicated tab. It returns when ctx is cancelled.
	Keep(ctx context.Context) error
}

// tabPool is a counting semaphore sized to the WORK tab count T. Acquire blocks
// (ctx-aware) until a slot frees, bounding concurrent Do() calls to exactly T.
type tabPool struct{ slots chan struct{} }

func newTabPool(n int) *tabPool {
	if n < 1 {
		n = 1
	}
	p := &tabPool{slots: make(chan struct{}, n)}
	for i := 0; i < n; i++ {
		p.slots <- struct{}{}
	}
	return p
}

func (p *tabPool) acquire(ctx context.Context) error {
	select {
	case <-p.slots:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *tabPool) release() { p.slots <- struct{}{} }

// size returns the work-pool capacity T (excludes any keep-alive tab).
func (p *tabPool) size() int { return cap(p.slots) }

// playwrightSession is the production BrowserSession over a single Chromium
// context with the authenticated storageState injected. All Do() requests reuse
// that one context, so every worker's traffic carries the same single session
// (the single-session invariant, PR-12). Chromium provisioning + `playwright
// install` are a build/Infra concern (honest scope).
type playwrightSession struct {
	pw       *playwright.Playwright
	browser  playwright.Browser
	bctx     playwright.BrowserContext
	pool     *tabPool
	keepPage playwright.Page // reserved keep-alive tab (nil unless ReserveKeepAlive)
	keeper   LoginKeeper
}

// NewPlaywrightSession launches Chromium, opens ONE context with the
// storageState, sizes the work tab pool to cfg.Tabs, and (if requested) reserves
// a separate keep-alive tab. It is exercised in production, not CI (tests use
// FakeSession); it must remain compilable and API-correct.
func NewPlaywrightSession(cfg SessionConfig, opts ...SessionOption) (platform.BrowserSession, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("proxy: playwright run: %w", err)
	}
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		_ = pw.Stop()
		return nil, fmt.Errorf("proxy: launch chromium: %w", err)
	}
	ctxOpts := playwright.BrowserNewContextOptions{}
	if cfg.StorageStatePath != "" {
		ctxOpts.StorageStatePath = playwright.String(cfg.StorageStatePath)
	}
	bctx, err := browser.NewContext(ctxOpts)
	if err != nil {
		_ = browser.Close()
		_ = pw.Stop()
		return nil, fmt.Errorf("proxy: new context: %w", err)
	}
	s := &playwrightSession{pw: pw, browser: browser, bctx: bctx, pool: newTabPool(cfg.Tabs)}
	s.applyOpts(opts)

	if cfg.ReserveKeepAlive {
		// Reserve a dedicated tab OUTSIDE the work pool for the login keeper.
		page, err := bctx.NewPage()
		if err != nil {
			_ = s.Close(context.Background())
			return nil, fmt.Errorf("proxy: reserve keep-alive tab: %w", err)
		}
		s.keepPage = page
		if s.keeper != nil {
			go func() { _ = s.keeper.Keep(context.Background()) }()
		}
	}
	return s, nil
}

// SessionOption customizes a BrowserSession.
type SessionOption func(*sessionOpts)
type sessionOpts struct{ keeper LoginKeeper }

// WithLoginKeeper injects the teammate's future login-agent. The keeper drives
// the reserved keep-alive tab; it is only started when ReserveKeepAlive is set.
func WithLoginKeeper(k LoginKeeper) SessionOption {
	return func(o *sessionOpts) { o.keeper = k }
}

func (s *playwrightSession) applyOpts(opts []SessionOption) {
	var o sessionOpts
	for _, opt := range opts {
		opt(&o)
	}
	s.keeper = o.keeper
}

func (s *playwrightSession) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if err := s.pool.acquire(ctx); err != nil { // bounded by T work tabs
		return nil, err
	}
	defer s.pool.release()

	headers := map[string]string{}
	for k := range req.Header {
		headers[k] = req.Header.Get(k)
	}
	resp, err := s.bctx.Request().Fetch(req.URL.String(), playwright.APIRequestContextFetchOptions{
		Method:  playwright.String(req.Method),
		Headers: headers,
	})
	if err != nil {
		return nil, fmt.Errorf("proxy: fetch %s: %w", req.URL, err)
	}
	body, err := resp.Body()
	if err != nil {
		return nil, fmt.Errorf("proxy: read body: %w", err)
	}
	hdr := http.Header{}
	for k, v := range resp.Headers() {
		hdr.Set(k, v)
	}
	return &http.Response{
		StatusCode: resp.Status(),
		Status:     resp.StatusText(),
		Header:     hdr,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}, nil
}

func (s *playwrightSession) Close(_ context.Context) error {
	if s.keepPage != nil {
		_ = s.keepPage.Close()
	}
	if s.bctx != nil {
		_ = s.bctx.Close()
	}
	if s.browser != nil {
		_ = s.browser.Close()
	}
	if s.pw != nil {
		return s.pw.Stop()
	}
	return nil
}

// FakeSession is the test double: it enforces the SAME work tab pool of size T
// and proves session injection, but does no real browser I/O — it forwards
// through an injected handler. Used by proxy + integration tests so CI needs no
// Chromium.
type FakeSession struct {
	pool       *tabPool
	authHeader string
	handler    func(ctx context.Context, req *http.Request) (*http.Response, error)

	mu       sync.Mutex
	inflight int
	maxSeen  int
}

// NewFakeSession builds a fake session with a work pool of cfg.Tabs. handler
// issues the (already session-injected) request. The keep-alive reservation is
// honored structurally: it never changes the work-pool size (that is the whole
// point of the constraint, so the test can assert max concurrency == T).
func NewFakeSession(cfg SessionConfig, authHeader string, handler func(context.Context, *http.Request) (*http.Response, error)) *FakeSession {
	return &FakeSession{pool: newTabPool(cfg.Tabs), authHeader: authHeader, handler: handler}
}

var _ platform.BrowserSession = (*FakeSession)(nil)
var _ platform.BrowserSession = (*playwrightSession)(nil)

func (f *FakeSession) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if err := f.pool.acquire(ctx); err != nil {
		return nil, err
	}
	defer f.pool.release()

	f.mu.Lock()
	f.inflight++
	if f.inflight > f.maxSeen {
		f.maxSeen = f.inflight
	}
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.inflight--; f.mu.Unlock() }()

	// Prove session injection: every forwarded request carries the auth marker.
	req.Header.Set("X-Auth-Session", f.authHeader)
	return f.handler(ctx, req)
}

func (f *FakeSession) Close(context.Context) error { return nil }

// MaxConcurrency returns the peak number of simultaneous Do() calls observed —
// used by tests to assert the work cap equals T regardless of keep-alive.
func (f *FakeSession) MaxConcurrency() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxSeen
}

// PoolSize returns the work-pool capacity T.
func (f *FakeSession) PoolSize() int { return f.pool.size() }
