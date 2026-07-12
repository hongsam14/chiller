// Package safety is the isolated security-critical module (SECURITY-11): the
// pre-approval gate (US-004, fail-closed) and the per-host rate limiter
// (US-005). Nothing goes outbound to an unapproved host, and no host is hit
// faster than its budget.
package safety

import (
	"errors"
	"sync"
	"time"

	"github.com/enki-offen/chiller/internal/domain"
)

// ErrNotApproved is returned for a target outside the effective approval set.
var ErrNotApproved = errors.New("safety: target not approved")

// ApprovalGate authorizes a target before any outbound request (SR-3, SEC-15).
type ApprovalGate interface{ Authorize(domain.Target) error }

type gate struct{ approved map[string]bool }

// NewApprovalGate builds the effective approval set: the per-scan Approved
// hosts, intersected with the global allowlist when one is configured
// (질문4=A). An empty/absent global allowlist ⇒ the request set governs.
func NewApprovalGate(requestApproved, globalAllowlist []string) ApprovalGate {
	reqSet := hostSet(requestApproved)
	if len(globalAllowlist) == 0 {
		return &gate{approved: reqSet}
	}
	glob := hostSet(globalAllowlist)
	eff := map[string]bool{}
	for h := range reqSet {
		if glob[h] {
			eff[h] = true
		}
	}
	return &gate{approved: eff}
}

func (g *gate) Authorize(t domain.Target) error {
	if g.approved[t.Host] {
		return nil
	}
	return ErrNotApproved // fail-closed
}

func hostSet(hosts []string) map[string]bool {
	s := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		s[h] = true
	}
	return s
}

// RateLimiter throttles outbound requests per host (US-005, NFR-13, SR-4).
type RateLimiter interface {
	// Acquire blocks until a token is available for host, then returns. The
	// release func is reserved (no-op now; a future per-host concurrency cap
	// would free a slot here).
	Acquire(host string) (release func(), err error)
}

type limiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	buckets map[string]*bucket
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter builds a per-host token-bucket limiter. rate/burst come from
// ScanOptions; a non-positive rate disables limiting. When limiting is active
// (rate>0) burst is clamped to at least 1 — a burst<1 could never accumulate a
// whole token, so Acquire would spin forever.
func NewRateLimiter(ratePerSec, burst int) RateLimiter {
	b := float64(burst)
	if ratePerSec > 0 && b < 1 {
		b = 1
	}
	return &limiter{rate: float64(ratePerSec), burst: b, buckets: map[string]*bucket{}, now: time.Now}
}

func (l *limiter) Acquire(host string) (func(), error) {
	noop := func() {}
	if l.rate <= 0 {
		return noop, nil // limiting disabled
	}
	for {
		wait := l.reserve(host)
		if wait <= 0 {
			return noop, nil
		}
		time.Sleep(wait)
	}
}

// reserve consumes a token if available (refilling by elapsed time); otherwise
// returns how long to wait for the next token.
func (l *limiter) reserve(host string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()
	b := l.buckets[host]
	if b == nil {
		b = &bucket{tokens: l.burst, last: t}
		l.buckets[host] = b
	}
	b.tokens += t.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = t
	if b.tokens >= 1 {
		b.tokens--
		return 0
	}
	need := 1 - b.tokens
	return time.Duration(need / l.rate * float64(time.Second))
}
