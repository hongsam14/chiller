package store

import (
	"context"
	"testing"
	"time"
)

// The backoff guard: sleep must never collapse to a zero delay.
//
// sleep computes `base << uint(2*(attempt-1))`. With attempt < 1 the shift count
// wraps to a huge uint and the duration becomes 0 — an unbounded retry storm
// against a store that is already failing. Every caller today passes attempt >= 1,
// so this is a latent trap rather than a live bug; the guard moves the safety from
// the caller into the function, and this test is what keeps it there.
//
// This test lives in package `store` (not `store_test`) because `sleep` and `rng`
// are unexported.
func TestSleepNeverCollapsesToZeroBackoff(t *testing.T) {
	r := WithRetry(NewInMemory(), 3, 50*time.Millisecond)
	r.rng = func() float64 { return 0 } // no jitter: measure the base delay alone

	for _, attempt := range []int{-1, 0, 1} {
		start := time.Now()
		if err := r.sleep(context.Background(), attempt); err != nil {
			t.Fatalf("sleep(%d) = %v, want nil", attempt, err)
		}
		if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
			t.Fatalf("sleep(%d) waited %v, want ~50ms: the backoff collapsed to zero", attempt, elapsed)
		}
	}
}
