package store_test

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/enki-offen/chiller/internal/crypto"
	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/findings/store"
)

// flaky fails its first failN SaveFindings calls, then succeeds.
type flaky struct {
	failN int
	err   error
	calls int
	saved []domain.Finding
}

func (f *flaky) SaveFindings(_ context.Context, _ domain.ScanID, fs []domain.Finding) error {
	f.calls++
	if f.calls <= f.failN {
		return f.err
	}
	f.saved = fs
	return nil
}

func (f *flaky) LoadFindings(context.Context, domain.ScanID) ([]domain.Finding, error) {
	return f.saved, nil
}

func TestRetry_TransientErrorEventuallySucceeds(t *testing.T) {
	inner := &flaky{failN: 2, err: errors.New("connection reset by peer")}
	r := store.WithRetry(inner, 3, time.Millisecond)

	want := []domain.Finding{fnd("humble", domain.SevHigh, "x")}
	if err := r.SaveFindings(context.Background(), "scan-1", want); err != nil {
		t.Fatalf("should have recovered after 2 transient failures: %v", err)
	}
	if inner.calls != 3 {
		t.Errorf("made %d attempts, want 3", inner.calls)
	}
	if got := r.Retries.Load(); got != 2 {
		t.Errorf("counted %d retries, want 2", got)
	}
	if r.Failures.Load() != 0 {
		t.Error("a save that eventually succeeded must not count as a failure")
	}
}

func TestRetry_GivesUpAfterAttempts(t *testing.T) {
	inner := &flaky{failN: 99, err: errors.New("still down")}
	r := store.WithRetry(inner, 3, time.Millisecond)

	err := r.SaveFindings(context.Background(), "scan-1", nil)
	if err == nil {
		t.Fatal("exhausting the attempts must surface the error, not swallow it")
	}
	if inner.calls != 3 {
		t.Errorf("made %d attempts, want 3", inner.calls)
	}
	if r.Failures.Load() != 1 {
		t.Errorf("counted %d failures, want 1", r.Failures.Load())
	}
}

// TestRetry_CancelledContextIsNotRetried pins the distinction that makes the
// backoff safe: a cancelled scan must fail immediately rather than sit through
// three sleeps first.
func TestRetry_CancelledContextIsNotRetried(t *testing.T) {
	inner := &flaky{failN: 99, err: context.Canceled}
	r := store.WithRetry(inner, 3, time.Second) // a real backoff: the test would hang if we slept

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() { done <- r.SaveFindings(ctx, "scan-1", nil) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("want an error")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("a cancelled context was retried through the backoff instead of failing fast")
	}
	if inner.calls != 1 {
		t.Errorf("made %d attempts on a cancelled context, want 1", inner.calls)
	}
}

func TestRetry_LoadIsNotRetried(t *testing.T) {
	// Reads are on a request path; a failure there should surface immediately.
	inner := &flaky{failN: 99, err: errors.New("down")}
	r := store.WithRetry(inner, 3, time.Millisecond)
	if _, err := r.LoadFindings(context.Background(), "scan-1"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if inner.calls != 0 {
		t.Error("LoadFindings must not go through the save retry loop")
	}
}

func TestRetry_ContractHolds(t *testing.T) {
	// The decorator must be transparent: wrapping a store must not change what it
	// does, only how hard it tries.
	runFindingsStoreContract(t, func() store.FindingsStore {
		return store.WithRetry(store.NewInMemory(), 3, time.Millisecond)
	})
}

// TestNewMongo_RejectsSubSecondTTL: expireAfterSeconds is an int32 of SECONDS, so a
// sub-second TTL rounds to 0, which Mongo reads as "expire at createdAt" — every
// finding deleted within a minute of the scan finishing. Refuse it rather than
// silently shredding the results. (No Mongo needed: the guard runs before connect.)
func TestNewMongo_RejectsSubSecondTTL(t *testing.T) {
	kr := testKeyringUnit(t)
	if _, err := store.NewMongo(context.Background(), "mongodb://127.0.0.1:1/", "db", kr, 900*time.Millisecond); err == nil {
		t.Error("a sub-second findings TTL must be rejected, not rounded to 'expire immediately'")
	}
}

func TestNewMongo_RequiresAKeyring(t *testing.T) {
	if _, err := store.NewMongo(context.Background(), "mongodb://127.0.0.1:1/", "db", nil, 0); err == nil {
		t.Error("the durable store must refuse to start without a key: it persists evidence")
	}
}

func testKeyringUnit(t *testing.T) crypto.Keyring {
	t.Helper()
	key := make([]byte, crypto.KeySize)
	kr, err := crypto.NewKeyring(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return kr
}
