//go:build integration

// Integration test for the durable Valkey WorkQueue. It runs the same contract
// suite as the mem impl (F-2/PR-11a) against a real Redis-compatible server, plus
// a Stats smoke check. Requires Docker; excluded from the default build via the
// `integration` tag (Q10=A).
package workqueue_test

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/platform"
	"github.com/hongsam14/chiller/internal/platform/workqueue"
	"github.com/testcontainers/testcontainers-go"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func startValkey(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	addr, err := ctr.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	return addr
}

func fastOpts(suffix string) workqueue.Options {
	return workqueue.Options{
		Stream: "s_" + suffix,
		Group:  "g_" + suffix,
		// Consume starts the reader that feeds Dequeue. Without it this suite hung
		// for the full test timeout instead of failing: a queue nobody reads looks
		// exactly like a queue with nothing in it.
		Consume:     true,
		BlockDur:    300 * time.Millisecond,
		ReaperEvery: 200 * time.Millisecond,
		MinIdle:     150 * time.Millisecond,
	}
}

func TestValkey_WorkQueueContract(t *testing.T) {
	addr := startValkey(t)
	ctx := context.Background()
	var n int64
	runWorkQueueContract(t, func() platform.WorkQueue {
		suffix := strconv.FormatInt(atomic.AddInt64(&n, 1), 10)
		v, err := workqueue.NewValkey(ctx, addr, "c_"+suffix, fastOpts(suffix))
		if err != nil {
			t.Fatalf("NewValkey: %v", err)
		}
		t.Cleanup(func() { _ = v.Close() })
		return v
	})
}

func TestValkey_StatsReflectsDepth(t *testing.T) {
	addr := startValkey(t)
	ctx := context.Background()
	v, err := workqueue.NewValkey(ctx, addr, "c_stats", fastOpts("stats"))
	if err != nil {
		t.Fatalf("NewValkey: %v", err)
	}
	defer func() { _ = v.Close() }()

	insp, ok := interface{}(v).(platform.QueueInspector)
	if !ok {
		t.Fatal("Valkey must implement QueueInspector")
	}
	for i := 0; i < 5; i++ {
		if err := v.Enqueue(ctx, domain.WorkItem{
			ID: domain.WorkItemID(strconv.Itoa(i)), ScanID: "s", Scope: domain.URLScoped,
			Target: domain.Target{URL: "https://x.test/" + strconv.Itoa(i), Host: "x.test", Scheme: "https"},
		}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	// Give the reader a moment; some may already be delivered/acked, so assert the
	// invariant depth+pending covers what was enqueued and not yet done.
	time.Sleep(100 * time.Millisecond)
	st, err := insp.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Depth+st.Pending <= 0 {
		t.Fatalf("expected some depth or pending after 5 enqueues, got %+v", st)
	}
}
