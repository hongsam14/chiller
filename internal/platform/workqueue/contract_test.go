package workqueue_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/platform"
	"github.com/enki-offen/chiller/internal/platform/workqueue"
)

// runWorkQueueContract is the implementation-independent behavioral suite for
// platform.WorkQueue (F-2). Mem and the durable Valkey impl run the identical
// suite (PR-11a). Assertions are written against OBSERVABLE behavior only, so a
// backend whose Drain is lazy (Valkey: mark-and-skip) still satisfies the same
// contract as the eager mem Drain.
func runWorkQueueContract(t *testing.T, newQueue func() platform.WorkQueue) {
	t.Helper()
	t.Run("EnqueueDequeue_FIFO", func(t *testing.T) { testFIFO(t, newQueue()) })
	t.Run("Dequeue_BlocksUntilEnqueue", func(t *testing.T) { testBlocks(t, newQueue()) })
	t.Run("Dequeue_CtxCancelUnblocks", func(t *testing.T) { testCtxCancel(t, newQueue()) })
	t.Run("Drain_DropsScanItems", func(t *testing.T) { testDrain(t, newQueue()) })
	t.Run("PBT_EnqueueDequeueMultiset", func(t *testing.T) { testEnqDeqMultiset(t, newQueue()) })
}

// A single dequeuer observes items in enqueue order (an ordered stream / FIFO).
func testFIFO(t *testing.T, q platform.WorkQueue) {
	ctx := context.Background()
	for _, id := range []domain.WorkItemID{"a", "b", "c"} {
		_ = q.Enqueue(ctx, domain.WorkItem{ID: id, ScanID: "s"})
	}
	for _, want := range []domain.WorkItemID{"a", "b", "c"} {
		got, err := q.Dequeue(ctx)
		if err != nil || got.ID != want {
			t.Fatalf("dequeue = %q,%v want %q", got.ID, err, want)
		}
	}
}

func testBlocks(t *testing.T, q platform.WorkQueue) {
	done := make(chan domain.WorkItem, 1)
	go func() {
		it, err := q.Dequeue(context.Background())
		if err != nil {
			t.Errorf("dequeue: %v", err)
		}
		done <- it
	}()
	select {
	case <-done:
		t.Fatal("dequeue returned before any enqueue")
	case <-time.After(20 * time.Millisecond):
	}
	_ = q.Enqueue(context.Background(), domain.WorkItem{ID: "x", ScanID: "s"})
	select {
	case it := <-done:
		if it.ID != "x" {
			t.Fatalf("got %q, want x", it.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dequeue did not unblock after enqueue")
	}
}

func testCtxCancel(t *testing.T, q platform.WorkQueue) {
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := q.Dequeue(ctx)
		errc <- err
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not unblock Dequeue")
	}
}

// After Drain(s1), no s1 item is ever dequeued; s2's item still is. (mem drops
// eagerly; Valkey marks-and-skips — same observable outcome.)
func testDrain(t *testing.T, q platform.WorkQueue) {
	ctx := context.Background()
	_ = q.Enqueue(ctx, domain.WorkItem{ID: "s1a", ScanID: "s1"})
	_ = q.Enqueue(ctx, domain.WorkItem{ID: "s2a", ScanID: "s2"})
	_ = q.Enqueue(ctx, domain.WorkItem{ID: "s1b", ScanID: "s1"})

	if err := q.Drain(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	got, err := q.Dequeue(ctx)
	if err != nil || got.ID != "s2a" {
		t.Fatalf("dequeue after drain = %q,%v want s2a", got.ID, err)
	}
	// A drained scan's items are never dequeued (whether dropped or skipped).
	_ = q.Enqueue(ctx, domain.WorkItem{ID: "s1c", ScanID: "s1"})
	dctx, cancel := context.WithTimeout(ctx, 60*time.Millisecond)
	defer cancel()
	if _, err := q.Dequeue(dctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected no dequeueable item after drain, got err=%v", err)
	}
}

// PBT: N enqueued items are all dequeued exactly once (multiset preserved). A
// single dequeuer additionally observes them in order.
func testEnqDeqMultiset(t *testing.T, q platform.WorkQueue) {
	const seed = 0xB0A7
	rng := rand.New(rand.NewSource(seed))
	t.Logf("PBT seed = %#x", seed)
	ctx := context.Background()

	n := 50 + rng.Intn(100)
	want := make([]domain.WorkItemID, n)
	for i := 0; i < n; i++ {
		id := domain.WorkItemID(fmt.Sprintf("wi-%d", i))
		want[i] = id
		if err := q.Enqueue(ctx, domain.WorkItem{ID: id, ScanID: "s"}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	for i := 0; i < n; i++ {
		dctx, cancel := context.WithTimeout(ctx, time.Second)
		got, err := q.Dequeue(dctx)
		cancel()
		if err != nil {
			t.Fatalf("dequeue %d: %v", i, err)
		}
		if got.ID != want[i] {
			t.Fatalf("dequeue %d = %q, want %q", i, got.ID, want[i])
		}
	}
}

func TestMem_WorkQueueContract(t *testing.T) {
	runWorkQueueContract(t, func() platform.WorkQueue { return workqueue.New() })
}
