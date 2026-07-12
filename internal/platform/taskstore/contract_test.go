package taskstore_test

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/platform"
	"github.com/enki-offen/chiller/internal/platform/taskstore"
	"github.com/enki-offen/chiller/internal/scan/ingest"
)

// runTaskStoreContract is the implementation-independent behavioral suite for
// platform.TaskStore (F-2). Both the in-memory Mem (below) and the durable Mongo
// impl (integration test) run the EXACT same suite by passing their constructor,
// so a backend swap must reproduce identical observable behavior (PR-11a). The
// mem impl doubles as the oracle for the property tests (PBT-05).
func runTaskStoreContract(t *testing.T, newStore func() platform.TaskStore) {
	t.Helper()
	t.Run("Claim_SingleWinner", func(t *testing.T) { testClaimSingleWinner(t, newStore()) })
	t.Run("StateCAS_GuardsWrongExpected", func(t *testing.T) { testStateCASGuards(t, newStore()) })
	t.Run("UnfinishedLedgerAndOutcomes", func(t *testing.T) { testUnfinishedLedger(t, newStore()) })
	t.Run("ResumeDoesNotReclaimDoneItem", func(t *testing.T) { testResumeNoReclaim(t, newStore()) })
	t.Run("ListScansByState", func(t *testing.T) { testListScansByState(t, newStore()) })
	t.Run("LeaseRefreshOwnerGuard", func(t *testing.T) { testLeaseRefreshGuard(t, newStore()) })
	t.Run("ListExpiredInflight", func(t *testing.T) { testListExpiredInflight(t, newStore()) })
	t.Run("RecordOutcomeOwnerGuard", func(t *testing.T) { testRecordOwnerGuard(t, newStore()) })
	t.Run("PBT_SerializationRoundTrip", func(t *testing.T) { testScanRoundTrip(t, newStore()) })
	t.Run("PBT_LedgerModelInvariants", func(t *testing.T) { testLedgerModel(t, newStore()) })
	t.Run("PBT_SafeReclaimInvariant", func(t *testing.T) { testSafeReclaim(t, newStore()) })
}

// claim is the lease-aware claim helper used across the suite: exactly the
// worker's take (pending→inflight + lease stamp).
func claim(t *testing.T, s platform.TaskStore, wid domain.WorkItemID, owner string, expiry domain.Timestamp) bool {
	t.Helper()
	// now=0 so the steal branch (leaseExpiry<now) never fires: this helper claims
	// PENDING items; steal is exercised explicitly in the lease/PBT tests.
	ok, err := s.ClaimItem(context.Background(), wid, platform.Lease{Owner: owner, Expiry: expiry}, 0)
	if err != nil {
		t.Fatalf("ClaimItem %q: %v", wid, err)
	}
	return ok
}

// ListScansByState returns exactly the scans in the requested state — the
// recovery sweep's source of running scans.
func testListScansByState(t *testing.T, s platform.TaskStore) {
	ctx := context.Background()
	mustSave := func(id domain.ScanID, st platform.ScanState) {
		if err := s.SaveScan(ctx, platform.Scan{ID: id, State: st, SchemaVersion: 1}); err != nil {
			t.Fatal(err)
		}
	}
	mustSave("r1", platform.StateRunning)
	mustSave("r2", platform.StateRunning)
	mustSave("c1", platform.StateCompleted)

	running, err := s.ListScansByState(ctx, platform.StateRunning)
	if err != nil {
		t.Fatal(err)
	}
	got := map[domain.ScanID]bool{}
	for _, id := range running {
		got[id] = true
	}
	if len(running) != 2 || !got["r1"] || !got["r2"] || got["c1"] {
		t.Fatalf("running = %v, want {r1,r2}", running)
	}
	completed, _ := s.ListScansByState(ctx, platform.StateCompleted)
	if len(completed) != 1 || completed[0] != "c1" {
		t.Fatalf("completed = %v, want [c1]", completed)
	}
}

func seedScan(t *testing.T, s platform.TaskStore, id domain.ScanID, items ...domain.WorkItemID) {
	t.Helper()
	ctx := context.Background()
	if err := s.SaveScan(ctx, platform.Scan{ID: id, State: platform.StateRunning, SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	wis := make([]domain.WorkItem, len(items))
	for i, wid := range items {
		wis[i] = domain.WorkItem{ID: wid, ScanID: id, Scope: domain.URLScoped}
	}
	if err := s.AppendWorkItems(ctx, id, wis); err != nil {
		t.Fatal(err)
	}
}

// Exactly one of many concurrent ClaimItem callers may win (pending→inflight +
// lease stamp is atomic).
func testClaimSingleWinner(t *testing.T, s platform.TaskStore) {
	ctx := context.Background()
	seedScan(t, s, "scan-1", "wi-1")

	const claimers = 32
	var wins int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := s.ClaimItem(ctx, "wi-1", platform.Lease{Owner: fmt.Sprintf("o%d", i), Expiry: 1_000_000}, 0)
			if err != nil {
				t.Errorf("claim: %v", err)
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("expected exactly 1 claim winner, got %d", wins)
	}
}

func testStateCASGuards(t *testing.T, s platform.TaskStore) {
	ctx := context.Background()
	if err := s.SaveScan(ctx, platform.Scan{ID: "scan-1", State: platform.StateCreated, SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	ok, err := s.CompareAndSetState(ctx, "scan-1", platform.StateRunning, platform.StateCompleted)
	if err != nil || ok {
		t.Fatalf("CAS with wrong expected must not swap: ok=%v err=%v", ok, err)
	}
	ok, err = s.CompareAndSetState(ctx, "scan-1", platform.StateCreated, platform.StateRunning)
	if err != nil || !ok {
		t.Fatalf("CAS with right expected must swap: ok=%v err=%v", ok, err)
	}
	got, _ := s.LoadScan(ctx, "scan-1")
	if got.State != platform.StateRunning {
		t.Fatalf("state = %v, want running", got.State)
	}
}

func testUnfinishedLedger(t *testing.T, s platform.TaskStore) {
	ctx := context.Background()
	seedScan(t, s, "scan-1", "wi-1", "wi-2")

	un, _ := s.ListUnfinished(ctx, "scan-1")
	if len(un) != 2 {
		t.Fatalf("initial unfinished = %d, want 2", len(un))
	}
	if !claim(t, s, "wi-1", "owner-a", 1_000_000) {
		t.Fatal("claim wi-1 failed")
	}
	un, _ = s.ListUnfinished(ctx, "scan-1")
	if len(un) != 2 {
		t.Fatalf("after claim, unfinished = %d, want 2 (inflight included)", len(un))
	}
	if ok, err := s.RecordOutcome(ctx, "wi-1", "owner-a", domain.WorkItemOutcome{ID: "wi-1"}); err != nil || !ok {
		t.Fatalf("RecordOutcome by owner: ok=%v err=%v", ok, err)
	}
	un, _ = s.ListUnfinished(ctx, "scan-1")
	if len(un) != 1 || un[0].ID != "wi-2" {
		t.Fatalf("after RecordOutcome, unfinished = %+v, want [wi-2]", un)
	}
	outs, _ := s.AllOutcomes(ctx, "scan-1")
	if len(outs) != 1 || outs[0].ID != "wi-1" {
		t.Fatalf("AllOutcomes = %+v, want [wi-1]", outs)
	}
	_ = claim(t, s, "wi-2", "owner-b", 1_000_000)
	_, _ = s.RecordOutcome(ctx, "wi-2", "owner-b", domain.WorkItemOutcome{ID: "wi-2"})
	un, _ = s.ListUnfinished(ctx, "scan-1")
	if len(un) != 0 {
		t.Fatalf("all done, unfinished = %d, want 0", len(un))
	}
}

func testResumeNoReclaim(t *testing.T, s platform.TaskStore) {
	ctx := context.Background()
	seedScan(t, s, "scan-1", "wi-1")
	_ = claim(t, s, "wi-1", "owner-a", 1_000_000)
	_, _ = s.RecordOutcome(ctx, "wi-1", "owner-a", domain.WorkItemOutcome{ID: "wi-1"})

	if claim(t, s, "wi-1", "owner-b", 1_000_000) {
		t.Fatal("a done item must not be re-claimable")
	}
}

// RefreshLease succeeds only while inflight and owned; ListExpiredInflight and
// RecordOutcome respect the lease owner (BR-L2/L4/L5).
func testLeaseRefreshGuard(t *testing.T, s platform.TaskStore) {
	ctx := context.Background()
	seedScan(t, s, "scan-1", "wi-1")
	if !claim(t, s, "wi-1", "owner-a", 100) {
		t.Fatal("claim failed")
	}
	// wrong owner cannot refresh
	if ok, _ := s.RefreshLease(ctx, "wi-1", "owner-b", 200); ok {
		t.Fatal("RefreshLease by a non-owner must fail")
	}
	// right owner extends
	if ok, _ := s.RefreshLease(ctx, "wi-1", "owner-a", 500); !ok {
		t.Fatal("RefreshLease by the owner must succeed")
	}
	// after extension, not expired at now=300
	exp, _ := s.ListExpiredInflight(ctx, "scan-1", 300)
	if len(exp) != 0 {
		t.Fatalf("refreshed item should not be expired at now=300, got %+v", exp)
	}
}

func testListExpiredInflight(t *testing.T, s platform.TaskStore) {
	ctx := context.Background()
	seedScan(t, s, "scan-1", "wi-1", "wi-2", "wi-3")
	_ = claim(t, s, "wi-1", "o1", 100) // expires at 100
	_ = claim(t, s, "wi-2", "o2", 500) // expires at 500
	// wi-3 stays pending

	exp, err := s.ListExpiredInflight(ctx, "scan-1", 300)
	if err != nil {
		t.Fatal(err)
	}
	if len(exp) != 1 || exp[0].ID != "wi-1" {
		t.Fatalf("expired at now=300 = %+v, want [wi-1] (pending & unexpired excluded)", exp)
	}
}

func testRecordOwnerGuard(t *testing.T, s platform.TaskStore) {
	ctx := context.Background()
	seedScan(t, s, "scan-1", "wi-1")
	_ = claim(t, s, "wi-1", "owner-a", 1_000_000)

	// a stale/wrong owner cannot record the outcome (BR-L5)
	if ok, _ := s.RecordOutcome(ctx, "wi-1", "owner-stale", domain.WorkItemOutcome{ID: "wi-1"}); ok {
		t.Fatal("RecordOutcome by a non-owner must be rejected")
	}
	// the item is still inflight (not finalized by the stale write)
	un, _ := s.ListUnfinished(ctx, "scan-1")
	if len(un) != 1 {
		t.Fatalf("item must remain unfinished after a rejected record, got %d", len(un))
	}
	// the real owner records fine
	if ok, _ := s.RecordOutcome(ctx, "wi-1", "owner-a", domain.WorkItemOutcome{ID: "wi-1"}); !ok {
		t.Fatal("RecordOutcome by the owner must succeed")
	}
}

// PBT-02 (round-trip): a saved Scan loads back equal on its persisted header
// fields. For Mem this is a struct copy; for Mongo it is a real bson round-trip,
// which is where field-tag/enum-string fidelity (Q4=A) is actually exercised.
func testScanRoundTrip(t *testing.T, s platform.TaskStore) {
	ctx := context.Background()
	const seed = 0xC0FFEE
	rng := rand.New(rand.NewSource(seed))
	t.Logf("PBT seed = %#x", seed)
	states := []platform.ScanState{
		platform.StateCreated, platform.StateRunning,
		platform.StateCompleted, platform.StateStopped, platform.StateFailed,
	}
	for i := 0; i < 200; i++ {
		want := platform.Scan{
			ID:            domain.ScanID(fmt.Sprintf("scan-rt-%d", i)),
			State:         states[rng.Intn(len(states))],
			Request:       genRequest(rng),
			Frameworks:    []domain.Framework{domain.FrameworkKISA},
			CreatedAt:     domain.Timestamp(rng.Int63n(1 << 40)),
			UpdatedAt:     domain.Timestamp(rng.Int63n(1 << 40)),
			SchemaVersion: 1,
		}
		if err := s.SaveScan(ctx, want); err != nil {
			t.Fatalf("SaveScan: %v", err)
		}
		got, err := s.LoadScan(ctx, want.ID)
		if err != nil {
			t.Fatalf("LoadScan: %v", err)
		}
		if got.State != want.State || got.CreatedAt != want.CreatedAt || got.UpdatedAt != want.UpdatedAt {
			t.Fatalf("round-trip header mismatch: got %+v want %+v", got, want)
		}
		if !equalStrings(got.Request.URLs, want.Request.URLs) ||
			got.Request.Options.RatePerHost != want.Request.Options.RatePerHost ||
			got.Request.Options.MaxURLs != want.Request.Options.MaxURLs {
			t.Fatalf("round-trip request mismatch: got %+v want %+v", got.Request, want.Request)
		}
	}
}

// PBT-05/06 (oracle + stateful): a random command sequence against the store is
// checked step-by-step against a minimal in-test model (the oracle). Invariant:
// an item is unfinished iff the model says pending|inflight; a done item never
// re-claims; AllOutcomes count matches recorded outcomes.
func testLedgerModel(t *testing.T, s platform.TaskStore) {
	ctx := context.Background()
	const seed = 0x5EED
	rng := rand.New(rand.NewSource(seed))
	t.Logf("PBT seed = %#x", seed)

	id := domain.ScanID("scan-model")
	if err := s.SaveScan(ctx, platform.Scan{ID: id, State: platform.StateRunning, SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	// model: wid -> state (0 pending, 1 inflight, 2 done)
	model := map[domain.WorkItemID]platform.ItemState{}
	var wids []domain.WorkItemID
	recorded := 0

	for step := 0; step < 500; step++ {
		switch rng.Intn(4) {
		case 0: // append a fresh item
			wid := domain.WorkItemID(fmt.Sprintf("wi-%d", len(wids)))
			if err := s.AppendWorkItems(ctx, id, []domain.WorkItem{{ID: wid, ScanID: id, Scope: domain.URLScoped}}); err != nil {
				t.Fatalf("append: %v", err)
			}
			model[wid] = platform.ItemPending
			wids = append(wids, wid)
		case 1: // try claim a random item
			if len(wids) == 0 {
				continue
			}
			wid := wids[rng.Intn(len(wids))]
			ok, err := s.ClaimItem(ctx, wid, platform.Lease{Owner: "w", Expiry: 1_000_000}, 0)
			if err != nil {
				t.Fatalf("claim: %v", err)
			}
			wantOK := model[wid] == platform.ItemPending
			if ok != wantOK {
				t.Fatalf("claim %s: store ok=%v, oracle=%v (state %v)", wid, ok, wantOK, model[wid])
			}
			if ok {
				model[wid] = platform.ItemInflight
			}
		case 2: // record outcome for a random inflight item
			if len(wids) == 0 {
				continue
			}
			wid := wids[rng.Intn(len(wids))]
			if model[wid] != platform.ItemInflight {
				continue
			}
			ok, err := s.RecordOutcome(ctx, wid, "w", domain.WorkItemOutcome{ID: wid})
			if err != nil {
				t.Fatalf("record: %v", err)
			}
			if !ok {
				t.Fatalf("record %s: owner should match, got rejected", wid)
			}
			model[wid] = platform.ItemDone
			recorded++
		case 3: // check ListUnfinished == model pending∪inflight
			un, err := s.ListUnfinished(ctx, id)
			if err != nil {
				t.Fatalf("unfinished: %v", err)
			}
			wantUnfinished := 0
			for _, st := range model {
				if st == platform.ItemPending || st == platform.ItemInflight {
					wantUnfinished++
				}
			}
			if len(un) != wantUnfinished {
				t.Fatalf("unfinished = %d, oracle = %d", len(un), wantUnfinished)
			}
		}
	}
	outs, _ := s.AllOutcomes(ctx, id)
	if len(outs) != recorded {
		t.Fatalf("AllOutcomes = %d, oracle recorded = %d", len(outs), recorded)
	}
}

// testSafeReclaim is the increment's headline property (PBT-05/06): under any
// interleaving of claim / lease-expiry / sweep-reclaim / re-claim / record —
// including stale-owner record attempts — NO item is ever recorded twice, and
// the store's accept/reject of each record matches the reference model. This is
// the safe-reclaim invariant the lease exists to guarantee.
func testSafeReclaim(t *testing.T, s platform.TaskStore) {
	ctx := context.Background()
	const seed = 0x1EA5E
	rng := rand.New(rand.NewSource(seed))
	t.Logf("PBT seed = %#x", seed)

	id := domain.ScanID("scan-safe")
	if err := s.SaveScan(ctx, platform.Scan{ID: id, State: platform.StateRunning, SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	const K = 8
	wids := make([]domain.WorkItemID, K)
	items := make([]domain.WorkItem, K)
	for i := 0; i < K; i++ {
		wids[i] = domain.WorkItemID(fmt.Sprintf("wi-%d", i))
		items[i] = domain.WorkItem{ID: wids[i], ScanID: id, Scope: domain.URLScoped}
	}
	if err := s.AppendWorkItems(ctx, id, items); err != nil {
		t.Fatal(err)
	}

	// reference model
	type mstate struct {
		state  platform.ItemState
		owner  string
		expiry int64
	}
	model := make([]mstate, K) // all pending, owner ""
	recordedCount := make([]int, K)
	clock := int64(1000)
	ownerSeq := 0
	const ttl = 60

	for step := 0; step < 1200; step++ {
		switch rng.Intn(5) {
		case 0: // claim-or-steal a random item (steal-on-claim, BR-L1/L4)
			i := rng.Intn(K)
			owner := fmt.Sprintf("o%d", ownerSeq)
			ownerSeq++
			ok, err := s.ClaimItem(ctx, wids[i], platform.Lease{Owner: owner, Expiry: domain.Timestamp(clock + ttl)}, domain.Timestamp(clock))
			if err != nil {
				t.Fatalf("claim: %v", err)
			}
			// claimable iff pending OR (inflight AND lease expired at `clock`)
			wantOK := model[i].state == platform.ItemPending ||
				(model[i].state == platform.ItemInflight && model[i].expiry < clock)
			if ok != wantOK {
				t.Fatalf("claim wi-%d: store=%v model=%v (state %v expiry %d clock %d)", i, ok, wantOK, model[i].state, model[i].expiry, clock)
			}
			if ok {
				model[i] = mstate{state: platform.ItemInflight, owner: owner, expiry: clock + ttl}
			}
		case 1: // advance the clock
			clock += int64(rng.Intn(40))
		case 2: // sweep: verify ListExpiredInflight returns only genuinely-expired
			// inflight items (the sweep re-enqueues them; steal-on-claim, not a
			// reset, does the reclaim — so no state change here).
			exp, err := s.ListExpiredInflight(ctx, id, domain.Timestamp(clock))
			if err != nil {
				t.Fatalf("list expired: %v", err)
			}
			for _, it := range exp {
				idx := index(wids, it.ID)
				if model[idx].state != platform.ItemInflight || model[idx].expiry >= clock {
					t.Fatalf("ListExpiredInflight returned non-expired wi-%d (model %+v, clock %d)", idx, model[idx], clock)
				}
			}
		case 3: // record with the CORRECT current owner (if inflight)
			i := rng.Intn(K)
			if model[i].state != platform.ItemInflight {
				continue
			}
			ok, err := s.RecordOutcome(ctx, wids[i], model[i].owner, domain.WorkItemOutcome{ID: wids[i]})
			if err != nil {
				t.Fatalf("record: %v", err)
			}
			if !ok {
				t.Fatalf("record wi-%d by current owner %q should succeed", i, model[i].owner)
			}
			model[i] = mstate{state: platform.ItemDone}
			recordedCount[i]++
			if recordedCount[i] > 1 {
				t.Fatalf("SAFE-RECLAIM VIOLATION: wi-%d recorded %d times", i, recordedCount[i])
			}
		case 4: // record with a STALE owner — must always be rejected
			i := rng.Intn(K)
			ok, err := s.RecordOutcome(ctx, wids[i], "stale-owner", domain.WorkItemOutcome{ID: wids[i]})
			if err != nil {
				t.Fatalf("stale record: %v", err)
			}
			if ok {
				t.Fatalf("record wi-%d by a stale owner must be rejected", i)
			}
		}
	}

	// Every item's outcome was recorded at most once; the store agrees.
	outs, _ := s.AllOutcomes(ctx, id)
	want := 0
	for i := 0; i < K; i++ {
		if recordedCount[i] > 1 {
			t.Fatalf("wi-%d recorded %d times", i, recordedCount[i])
		}
		want += recordedCount[i]
	}
	if len(outs) != want {
		t.Fatalf("AllOutcomes = %d, model recorded = %d", len(outs), want)
	}
}

func index(ws []domain.WorkItemID, w domain.WorkItemID) int {
	for i, x := range ws {
		if x == w {
			return i
		}
	}
	return -1
}

func genRequest(rng *rand.Rand) platform.ScanRequest {
	n := rng.Intn(4)
	urls := make([]string, n)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://h%d.example/%d", rng.Intn(5), i)
	}
	return platform.ScanRequest{
		URLs:     urls,
		Approved: []string{"example"},
		Options: ingest.ScanOptions{
			RatePerHost: rng.Intn(20),
			Burst:       rng.Intn(20),
			MaxURLs:     rng.Intn(500),
		},
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The mem unit test entry point — the durable backends run the same suite in
// their //go:build integration test files.
func TestMem_TaskStoreContract(t *testing.T) {
	runTaskStoreContract(t, func() platform.TaskStore { return taskstore.New() })
}
