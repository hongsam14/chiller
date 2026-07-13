package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/scan/safety"
	retryablehttp "github.com/projectdiscovery/retryablehttp-go"
)

const (
	defaultMaxBody = 512 * 1024       // NFR-16 bounded body
	defaultTimeout = 20 * time.Second // per-request deadline (SR-8, NFR-12)
	defaultRetries = 2                // transient-error retries (0 disables)
	retryWaitMin   = 200 * time.Millisecond
	retryWaitMax   = 2 * time.Second
)

// hostCtxKey carries the target host into the retry policy so a retry can charge
// the correct per-host rate budget even when the transport error yields a nil
// response (from which the host could not otherwise be recovered).
type hostCtxKey struct{}

// Fetcher fetches a URL once, gated + rate-limited, into the cache (SR-2/3/4).
// ctx carries the scan-level deadline/cancellation so an aborted scan promptly
// stops in-flight outbound traffic (SR-8).
type Fetcher interface {
	Fetch(ctx context.Context, scanID domain.ScanID, t domain.Target) (*domain.HAREntry, error)
}

type fetcher struct {
	gate     safety.ApprovalGate
	lim      safety.RateLimiter
	cache    ResponseCache
	client   *http.Client
	rc       *retryablehttp.Client
	maxBody  int64
	timeout  time.Duration
	proxyURL string
	retries  int
	stats    *CacheStats
}

// Option configures a Fetcher at construction (backward-compatible variadic;
// existing callers pass none).
type Option func(*fetcher)

// WithProxy routes the Fetcher's outbound requests through proxyURL — used in
// financial mode so ProxyEligible checkers egress through the single
// authenticated auth-proxy (PR-12/PR-12c). An empty proxyURL is a no-op.
func WithProxy(proxyURL string) Option {
	return func(f *fetcher) { f.proxyURL = proxyURL }
}

// WithCacheStats gives the Fetcher somewhere to record cache hits, misses and
// backend errors. Without it the Fetcher still works; it just reports nothing.
func WithCacheStats(s *CacheStats) Option {
	return func(f *fetcher) { f.stats = s }
}

// WithRetries sets the number of transient-error retries (conn reset / 5xx /
// timeout) with bounded exponential backoff (projectdiscovery/retryablehttp-go).
// 0 disables retries. Each retry attempt still spends a per-host rate token
// (SR-4) and each redirect hop is still re-gated (SR-3) — resilience never
// widens the safety envelope.
func WithRetries(n int) Option {
	return func(f *fetcher) {
		if n < 0 {
			n = 0
		}
		f.retries = n
	}
}

// NewFetcher builds a Fetcher. Every request is bounded by an explicit
// per-request timeout regardless of the passed client's own Timeout (SR-8), and
// redirects are re-gated per hop so no redirect reaches an unapproved host (SR-3).
func NewFetcher(gate safety.ApprovalGate, lim safety.RateLimiter, cache ResponseCache, client *http.Client, opts ...Option) Fetcher {
	if client == nil {
		client = &http.Client{}
	}
	// Re-authorize + charge the budget on every redirect hop; the initial gate
	// check only covers the first URL (fail-closed against redirect scope-escape).
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		hop := domain.Target{Host: req.URL.Hostname()}
		if err := gate.Authorize(hop); err != nil {
			return err
		}
		release, err := lim.Acquire(req.URL.Hostname())
		if err != nil {
			return err
		}
		release()
		return nil
	}
	f := &fetcher{gate: gate, lim: lim, cache: cache, client: client, maxBody: defaultMaxBody, timeout: defaultTimeout, retries: defaultRetries}
	for _, opt := range opts {
		opt(f)
	}
	if f.proxyURL != "" {
		if pu, err := url.Parse(f.proxyURL); err == nil {
			tr, ok := client.Transport.(*http.Transport)
			if !ok || tr == nil {
				tr = &http.Transport{}
			} else {
				tr = tr.Clone()
			}
			tr.Proxy = http.ProxyURL(pu)
			client.Transport = tr
		}
	}

	// Wrap the gated client with retry/backoff. Timeout: 0 leaves the per-request
	// ctx deadline (set in Fetch) as the single SR-8 bound covering all attempts.
	f.rc = retryablehttp.NewWithHTTPClient(client, retryablehttp.Options{
		RetryWaitMin:    retryWaitMin,
		RetryWaitMax:    retryWaitMax,
		RetryMax:        f.retries,
		RespReadLimit:   defaultMaxBody,
		NoAdjustTimeout: true,
		CheckRetry:      f.checkRetry,
		Backoff:         retryablehttp.DefaultBackoff(),
	})
	// retryablehttp keeps a second, ungated HTTP/2 fallback client used only on a
	// malformed-HTTP/2 transport error. Point it at the SAME gated client so the
	// redirect re-gate + proxy transport hold on EVERY path (no fail-open seam).
	f.rc.HTTPClient2 = client
	return f
}

// checkRetry decides whether to retry and, when it will, spends one per-host
// rate token so retries can never exceed the budget (SR-4). A scope/approval
// rejection is never transient and is failed fast (SR-3).
func (f *fetcher) checkRetry(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if err != nil && errors.Is(err, safety.ErrNotApproved) {
		return false, err
	}
	retry, rerr := retryablehttp.CheckRecoverableErrors(ctx, resp, err)
	if retry {
		if host, _ := ctx.Value(hostCtxKey{}).(string); host != "" {
			if release, aerr := f.lim.Acquire(host); aerr == nil {
				release()
			}
		}
	}
	return retry, rerr
}

func (f *fetcher) Fetch(ctx context.Context, scanID domain.ScanID, t domain.Target) (*domain.HAREntry, error) {
	// Reuse: never re-fetch a URL already cached this scan (FR-02).
	//
	// The cache fails open. If the backend is unreachable we treat it as a miss and
	// fetch again: the cache is an optimization, and a scan that runs slowly beats a
	// scan that does not run. What that costs is extra traffic to the target — which
	// is bounded anyway, because a refetch goes through the same approval gate and
	// the same per-host rate budget below as a first fetch. The error is counted, so
	// a cache that is failing every call cannot masquerade as one that is merely cold.
	switch e, hit, err := f.cache.Get(ctx, scanID, t.URL); {
	case err != nil:
		f.stats.err()
	case hit:
		f.stats.hit()
		return e, nil
	default:
		f.stats.miss()
	}
	if err := f.gate.Authorize(t); err != nil { // fail-closed BEFORE any outbound (SR-3)
		return nil, err
	}
	release, err := f.lim.Acquire(t.Host) // per-host budget (SR-4)
	if err != nil {
		return nil, err
	}
	defer release()

	// Explicit per-request deadline, derived from the scan ctx so a scan abort
	// also cancels this fetch (SR-8) — never relies on client.Timeout. The host
	// rides in the ctx so the retry policy can charge the right per-host budget.
	reqCtx := context.WithValue(ctx, hostCtxKey{}, t.Host)
	reqCtx, cancel := context.WithTimeout(reqCtx, f.timeout)
	defer cancel()
	req, err := retryablehttp.NewRequestWithContext(reqCtx, http.MethodGet, t.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	resp, err := f.rc.Do(req) // bounded transient-error retries + backoff
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", t.URL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBody))
	if err != nil {
		return nil, fmt.Errorf("fetch %s read: %w", t.URL, err)
	}
	e := toHAR(t, resp, body)
	// Do not cache a response for a scan that is already over. StopScan cancels the
	// scan context and then wipes the cache, but a fetch already past its ctx checks
	// keeps running to completion — and a Put here would land AFTER the wipe,
	// re-creating exactly the response body the wipe exists to remove and leaving it
	// there until the TTL catches it (FR-12).
	if ctx.Err() != nil {
		return e, nil
	}
	// A cache write that fails costs us reuse, not correctness — the response is
	// already in hand. Count it and move on.
	if err := f.cache.Put(ctx, scanID, t.URL, e); err != nil {
		f.stats.err()
	}
	return e, nil
}

func toHAR(t domain.Target, resp *http.Response, body []byte) *domain.HAREntry {
	headers := make([]domain.Header, 0, len(resp.Header))
	for name, vals := range resp.Header {
		for _, v := range vals {
			headers = append(headers, domain.Header{Name: name, Value: v})
		}
	}
	cookies := make([]domain.Cookie, 0)
	for _, c := range resp.Cookies() {
		cookies = append(cookies, domain.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Secure:   c.Secure,
			HttpOnly: c.HttpOnly,
			SameSite: sameSite(c.SameSite),
		})
	}
	return &domain.HAREntry{
		URL:       t.URL,
		Scheme:    t.Scheme,
		Status:    resp.StatusCode,
		Headers:   headers,
		Cookies:   cookies,
		Body:      body,
		FetchedAt: domain.Timestamp(time.Now().UnixMilli()),
	}
}

func sameSite(s http.SameSite) string {
	switch s {
	case http.SameSiteLaxMode:
		return "Lax"
	case http.SameSiteStrictMode:
		return "Strict"
	case http.SameSiteNoneMode:
		return "None"
	default:
		return "" // unset (PS-017 signal)
	}
}
