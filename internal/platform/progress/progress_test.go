package progress

import (
	"context"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

func TestMem_CountsPartitionFinishedItems(t *testing.T) {
	ctx := context.Background()
	p := New()
	if err := p.Init(ctx, "s", 3); err != nil {
		t.Fatal(err)
	}

	item := func(id domain.WorkItemID) domain.WorkItem {
		return domain.WorkItem{ID: id, ScanID: "s"}
	}
	// claim all three
	for _, id := range []domain.WorkItemID{"w1", "w2", "w3"} {
		_ = p.OnClaimed(ctx, item(id))
	}
	snap, _ := p.Snapshot(ctx, "s")
	if snap.Inflight != 3 {
		t.Fatalf("inflight = %d, want 3", snap.Inflight)
	}

	// w1 clean, w2 has a failed checker, w3 clean with an N/A checker
	_ = p.OnFinished(ctx, "s", domain.WorkItemOutcome{ID: "w1"})
	_ = p.OnFinished(ctx, "s", domain.WorkItemOutcome{ID: "w2", Failed: []domain.CheckerID{"observation:CWE-1"}})
	_ = p.OnFinished(ctx, "s", domain.WorkItemOutcome{
		ID:            "w3",
		NotApplicable: map[domain.CheckerID][]domain.CWE{"observation:CWE-2": {"CWE-2"}},
	})

	snap, _ = p.Snapshot(ctx, "s")
	if snap.Total != 3 || snap.Done != 2 || snap.Failed != 1 || snap.Inflight != 0 {
		t.Fatalf("snapshot = %+v, want total3 done2 failed1 inflight0", snap)
	}
	if snap.PerChecker["observation:CWE-1"].Failed != 1 {
		t.Errorf("failed checker not tallied: %+v", snap.PerChecker)
	}
	if snap.PerChecker["observation:CWE-2"].OK != 1 {
		t.Errorf("ok (n/a) checker not tallied: %+v", snap.PerChecker)
	}
}

func TestMem_Snapshot_UnknownScan_Empty(t *testing.T) {
	snap, err := New().Snapshot(context.Background(), "nope")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Total != 0 || len(snap.PerChecker) != 0 {
		t.Fatalf("unknown scan snapshot not empty: %+v", snap)
	}
}
