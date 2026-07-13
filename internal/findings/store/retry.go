package store

import (
	"context"
	"errors"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/enki-offen/chiller/internal/domain"
)

// Retry wraps a FindingsStore with bounded exponential backoff on SaveFindings.
//
// The write it guards happens at the very end of a scan, after all the work is
// already done: a transient blip on the backend at that moment would otherwise
// throw away the entire scan's results. Retrying is worth it precisely because the
// alternative is so expensive.
//
// It retries the SAVE only. LoadFindings is a read on a request path; a failure
// there surfaces immediately rather than stalling the caller.
type Retry struct {
	inner    FindingsStore
	attempts int
	base     time.Duration
	rng      func() float64 // jitter source; swappable in tests

	Retries  atomic.Int64 // findings_save_retries_total
	Failures atomic.Int64 // findings_save_failures_total
	LastMS   atomic.Int64 // findings_save_duration_ms (last successful save)
}

var _ FindingsStore = (*Retry)(nil)

// DefaultAttempts and DefaultBackoff give 100ms → 400ms → 1.6s (plus jitter).
const (
	DefaultAttempts = 3
	DefaultBackoff  = 100 * time.Millisecond
)

// WithRetry decorates inner. attempts <= 0 or base <= 0 fall back to the defaults.
func WithRetry(inner FindingsStore, attempts int, base time.Duration) *Retry {
	if attempts <= 0 {
		attempts = DefaultAttempts
	}
	if base <= 0 {
		base = DefaultBackoff
	}
	return &Retry{inner: inner, attempts: attempts, base: base, rng: rand.Float64}
}

func (r *Retry) SaveFindings(ctx context.Context, scanID domain.ScanID, findings []domain.Finding) error {
	start := time.Now()
	var err error
	for attempt := 0; attempt < r.attempts; attempt++ {
		if attempt > 0 {
			r.Retries.Add(1)
			if waitErr := r.sleep(ctx, attempt); waitErr != nil {
				r.Failures.Add(1)
				return waitErr
			}
		}
		err = r.inner.SaveFindings(ctx, scanID, findings)
		if err == nil {
			r.LastMS.Store(time.Since(start).Milliseconds())
			return nil
		}
		if !retryable(ctx, err) {
			// A malformed document or a cancelled context will fail again just as
			// fast; retrying only delays the bad news.
			r.Failures.Add(1)
			return err
		}
	}
	r.Failures.Add(1)
	return err
}

func (r *Retry) LoadFindings(ctx context.Context, scanID domain.ScanID) ([]domain.Finding, error) {
	return r.inner.LoadFindings(ctx, scanID)
}

// sleep waits out the backoff for the given attempt, or returns early if the
// context is done — a cancelled scan must not sit in a backoff loop.
func (r *Retry) sleep(ctx context.Context, attempt int) error {
	// The guard belongs here, not in the caller: with attempt < 1 the old
	// `base << uint(2*(attempt-1))` wrapped the shift count to a huge uint and
	// collapsed the backoff to zero — a retry storm against a store that is
	// already failing.
	if attempt < 1 {
		attempt = 1
	}
	// Quadruple per attempt (1x, 4x, 16x). Written as repeated multiplication
	// rather than a shift so there is no int→uint conversion to get wrong.
	d := r.base
	for i := 1; i < attempt; i++ {
		d *= 4
	}
	jitter := time.Duration(r.rng() * float64(d) * 0.2)
	t := time.NewTimer(d + jitter)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retryable reports whether err is worth another attempt. A dead context never is
// (the caller has given up); everything else is treated as possibly transient,
// because the Mongo driver reports network faults, primary step-downs and write
// timeouts as ordinary errors and enumerating them here would go stale.
func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}
