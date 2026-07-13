package safety

import (
	"testing"
	"time"

	"github.com/hongsam14/chiller/internal/domain"
)

func TestApprovalGate_RequestOnly(t *testing.T) {
	g := NewApprovalGate([]string{"a.test"}, nil)
	if err := g.Authorize(domain.Target{Host: "a.test"}); err != nil {
		t.Error("a.test should be approved")
	}
	if err := g.Authorize(domain.Target{Host: "evil.test"}); err == nil {
		t.Error("evil.test must be rejected (fail-closed)")
	}
}

func TestApprovalGate_Intersection(t *testing.T) {
	// request approves a,b but global allowlist only permits a → only a.
	g := NewApprovalGate([]string{"a.test", "b.test"}, []string{"a.test", "c.test"})
	if err := g.Authorize(domain.Target{Host: "a.test"}); err != nil {
		t.Error("a.test in both → approved")
	}
	if err := g.Authorize(domain.Target{Host: "b.test"}); err == nil {
		t.Error("b.test not in global allowlist → rejected")
	}
}

func TestRateLimiter_Disabled(t *testing.T) {
	l := NewRateLimiter(0, 0) // disabled
	rel, err := l.Acquire("h")
	if err != nil || rel == nil {
		t.Error("disabled limiter should pass")
	}
}

// M1 regression: rate>0 with burst<1 must clamp to 1, not spin forever waiting
// for a whole token that can never accumulate.
func TestRateLimiter_BurstClampedWhenActive(t *testing.T) {
	l := NewRateLimiter(1000, 0).(*limiter)
	if l.burst < 1 {
		t.Fatalf("burst must be clamped to >=1 when rate>0, got %v", l.burst)
	}
	// first Acquire returns immediately (has a token) rather than hanging.
	rel, err := l.Acquire("h")
	if err != nil || rel == nil {
		t.Error("clamped limiter should grant the first token immediately")
	}
}

func TestRateLimiter_BurstThenThrottle(t *testing.T) {
	l := NewRateLimiter(1000, 2).(*limiter)
	base := time.Unix(0, 0)
	l.now = func() time.Time { return base } // frozen clock
	// burst of 2 available immediately (two separate token consumptions)
	if l.reserve("h") != 0 {
		t.Fatal("burst token 1 should be immediate")
	}
	if l.reserve("h") != 0 {
		t.Fatal("burst token 2 should be immediate")
	}
	// 3rd requires waiting (no tokens at frozen time)
	if l.reserve("h") <= 0 {
		t.Error("3rd request should require a wait")
	}
	// per-host independence
	if l.reserve("other") != 0 {
		t.Error("a different host has its own bucket")
	}
}
