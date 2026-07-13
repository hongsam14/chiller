package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
	fconfig "github.com/enki-offen/chiller/internal/findings/config"
	"github.com/enki-offen/chiller/internal/findings/dedup"
	"github.com/enki-offen/chiller/internal/findings/report"
	fstore "github.com/enki-offen/chiller/internal/findings/store"
	"github.com/enki-offen/chiller/internal/platform"
	"github.com/enki-offen/chiller/internal/platform/progress"
	"github.com/enki-offen/chiller/internal/platform/taskstore"
	"github.com/enki-offen/chiller/internal/scan/checking/observation"
	"github.com/enki-offen/chiller/internal/scan/checking/probe"
	"github.com/enki-offen/chiller/internal/scan/fetch"
	"github.com/enki-offen/chiller/internal/scan/ingest"
)

// spyCache records what was wiped so a test can assert FR-12 actually happened.
type spyCache struct {
	*fetch.InMemory
	wiped   []domain.ScanID
	wipeErr error
}

func newSpyCache() *spyCache { return &spyCache{InMemory: fetch.NewInMemoryCache()} }

func (c *spyCache) WipeScan(ctx context.Context, id domain.ScanID) error {
	if c.wipeErr != nil {
		return c.wipeErr
	}
	c.wiped = append(c.wiped, id)
	return c.InMemory.WipeScan(ctx, id)
}

func testOrch(t *testing.T, store platform.TaskStore, findings fstore.FindingsStore, cache fetch.ResponseCache, metrics *platform.Metrics) *Orchestrator {
	t.Helper()
	mt, err := fconfig.LoadMappingFile(mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := fconfig.LoadCoverageSpecFile(coveragePath)
	if err != nil {
		t.Fatal(err)
	}
	checkers := append(observation.Checkers(observation.Default()), toCheckerSlice(probe.Default())...)
	acc, mapper, err := BootGate(checkers, mt, spec)
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{
		Store:         store,
		Queue:         nil,
		Progress:      progress.New(),
		Ingestor:      ingest.New(),
		Checkers:      checkers,
		Mapper:        mapper,
		Accountant:    acc,
		Deduper:       dedup.New(),
		ReportBuilder: report.New(mapper),
		FindingsStore: findings,
		Cache:         cache,
		Metrics:       metrics,
		Frameworks:    []domain.Framework{domain.FrameworkKISA},
	})
}

func seedFinishedScan(t *testing.T, store platform.TaskStore, id domain.ScanID) domain.Finding {
	t.Helper()
	ctx := context.Background()
	if err := store.SaveScan(ctx, platform.Scan{ID: id, State: platform.StateRunning}); err != nil {
		t.Fatal(err)
	}
	item := domain.WorkItem{ID: domain.WorkItemID(string(id) + "#0"), ScanID: id, Scope: domain.URLScoped}
	if err := store.AppendWorkItems(ctx, id, []domain.WorkItem{item}); err != nil {
		t.Fatal(err)
	}
	f := domain.Finding{
		CWEs:       []domain.CWE{"CWE-614"},
		TargetURL:  "https://x.test/login",
		SourceTool: "observation:CWE-614",
		Severity:   domain.SevMedium,
		Evidence:   domain.Evidence{Snippets: []string{"Set-Cookie: SESSION=s3cret"}},
	}
	// Claim then record, the way a worker does.
	lease := platform.Lease{Owner: "w1", Expiry: 1 << 40}
	if ok, err := store.ClaimItem(ctx, item.ID, lease, 0); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if ok, err := store.RecordOutcome(ctx, item.ID, "w1", domain.WorkItemOutcome{
		ID: item.ID, Findings: []domain.Finding{f},
	}); err != nil || !ok {
		t.Fatalf("record: ok=%v err=%v", ok, err)
	}
	return f
}

// TestFinalize_WipesCache is the regression for the defect this increment exists
// to fix: WipeScan was never called from anywhere, so FR-12/NFR-16 ("discard
// response bodies when the scan ends") did not actually happen. In the in-memory
// cache that was hidden by process exit; against a durable cache the bodies would
// simply stay there.
func TestFinalize_WipesCache(t *testing.T) {
	store := taskstore.New()
	cache := newSpyCache()
	o := testOrch(t, store, fstore.NewInMemory(), cache, &platform.Metrics{})
	ctx := context.Background()

	seedFinishedScan(t, store, "scan-done")
	_ = cache.Put(ctx, "scan-done", "https://x.test/login", &domain.HAREntry{URL: "https://x.test/login", Body: []byte("<html>")})

	o.finalize(ctx, "scan-done")

	if len(cache.wiped) != 1 || cache.wiped[0] != "scan-done" {
		t.Fatalf("finalize did not wipe the cache: %v", cache.wiped)
	}
	if _, hit, _ := cache.Get(ctx, "scan-done", "https://x.test/login"); hit {
		t.Error("a response body survived the end of the scan (FR-12)")
	}
}

// TestFail_WipesCache and TestStop_WipesCache: a scan that ends badly leaves
// exactly the same response bodies behind as one that ends well. All three
// terminal paths have to clean up, not just the happy one.
func TestFail_WipesCache(t *testing.T) {
	store := taskstore.New()
	cache := newSpyCache()
	o := testOrch(t, store, fstore.NewInMemory(), cache, &platform.Metrics{})
	ctx := context.Background()

	_ = store.SaveScan(ctx, platform.Scan{ID: "scan-failed", State: platform.StateRunning})
	_ = cache.Put(ctx, "scan-failed", "https://x.test/a", &domain.HAREntry{Body: []byte("<html>")})

	o.fail(ctx, "scan-failed")

	if len(cache.wiped) != 1 {
		t.Fatal("a failed scan left its response bodies in the cache")
	}
	if _, hit, _ := cache.Get(ctx, "scan-failed", "https://x.test/a"); hit {
		t.Error("a response body survived a failed scan (FR-12)")
	}
}

func TestStop_WipesCache(t *testing.T) {
	store := taskstore.New()
	cache := newSpyCache()
	o := testOrch(t, store, fstore.NewInMemory(), cache, &platform.Metrics{})
	o.cfg.Queue = noopQueue{}
	ctx := context.Background()

	_ = store.SaveScan(ctx, platform.Scan{ID: "scan-stopped", State: platform.StateRunning})
	_ = cache.Put(ctx, "scan-stopped", "https://x.test/a", &domain.HAREntry{Body: []byte("<html>")})

	if err := o.StopScan(ctx, "scan-stopped"); err != nil {
		t.Fatal(err)
	}
	if len(cache.wiped) != 1 {
		t.Fatal("a stopped scan left its response bodies in the cache")
	}
	if _, hit, _ := cache.Get(ctx, "scan-stopped", "https://x.test/a"); hit {
		t.Error("a response body survived a stopped scan (FR-12)")
	}
}

// TestWipeFailure_DoesNotFailTheScan: the wipe is best-effort. It must be counted
// (so a cache that never gets cleaned is visible) but must not turn a completed
// scan into a failed one — the results are already correct.
func TestWipeFailure_DoesNotFailTheScan(t *testing.T) {
	store := taskstore.New()
	cache := newSpyCache()
	cache.wipeErr = errors.New("valkey down")
	metrics := &platform.Metrics{}
	o := testOrch(t, store, fstore.NewInMemory(), cache, metrics)
	ctx := context.Background()

	seedFinishedScan(t, store, "scan-x")
	o.finalize(ctx, "scan-x")

	s, err := store.LoadScan(ctx, "scan-x")
	if err != nil {
		t.Fatal(err)
	}
	if s.State != platform.StateCompleted {
		t.Errorf("a failed cache wipe turned the scan %v; it should still be completed", s.State)
	}
	if metrics.CacheWipeErrors.Load() != 1 {
		t.Error("a failed wipe must be counted, or a cache that is never cleaned stays invisible")
	}
}

// TestFinalize_PurgesLedgerEvidence: once the findings are in their own store, the
// copy the ledger has been carrying is dead weight — nothing reads it again. Left
// alone it would be a second durable home for every response fragment, which is
// exactly the duplication this increment set out to remove.
func TestFinalize_PurgesLedgerEvidence(t *testing.T) {
	store := taskstore.New()
	findings := fstore.NewInMemory()
	o := testOrch(t, store, findings, newSpyCache(), &platform.Metrics{})
	ctx := context.Background()

	want := seedFinishedScan(t, store, "scan-purge")

	// Before finalize, the ledger is the only place the evidence exists.
	before, _ := store.AllOutcomes(ctx, "scan-purge")
	if len(before) != 1 || len(before[0].Findings[0].Evidence.Snippets) == 0 {
		t.Fatal("precondition: the ledger should be carrying the evidence")
	}

	o.finalize(ctx, "scan-purge")

	// After: the findings store has it...
	stored, err := findings.LoadFindings(ctx, "scan-purge")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || len(stored[0].Evidence.Snippets) != 1 || stored[0].Key() != want.Key() {
		t.Fatalf("the findings store should own the evidence now, got %+v", stored)
	}
	// ...and the ledger does not.
	after, _ := store.AllOutcomes(ctx, "scan-purge")
	if len(after) != 1 {
		t.Fatal("purge must not delete the outcome itself — resume and accounting still need it")
	}
	if len(after[0].Findings[0].Evidence.Snippets) != 0 {
		t.Error("the ledger is still holding a second copy of the evidence after finalize")
	}
	// The rest of the outcome survives: it is what the ledger is actually for.
	if after[0].ID != "scan-purge#0" {
		t.Error("purge damaged the outcome's identity")
	}
}

// TestFinalize_SecondFinalizerDoesNotDestroyEvidence is the regression for the
// worst bug in this increment. Workers finalize (whichever one drains a scan's last
// item does), maybeFinalize's guard is per-process, and the worker Deployment runs
// more than one replica — so two processes can finalize the same scan.
//
// The first one saves the findings and then purges the ledger's copy of the
// evidence. The second one then reads the ledger, gets the same findings with NO
// evidence, and saves again. Both halves of the fix are exercised here: only the
// CAS winner purges, and a store never lets an evidence-less save erase evidence it
// already holds.
//
// Two orchestrators over ONE shared store is exactly the two-pod topology.
func TestFinalize_SecondFinalizerDoesNotDestroyEvidence(t *testing.T) {
	store := taskstore.New()
	findings := fstore.NewInMemory()
	podA := testOrch(t, store, findings, newSpyCache(), &platform.Metrics{})
	podB := testOrch(t, store, findings, newSpyCache(), &platform.Metrics{})
	ctx := context.Background()

	want := seedFinishedScan(t, store, "scan-race")

	podA.finalize(ctx, "scan-race") // saves findings, wins the CAS, purges the ledger
	podB.finalize(ctx, "scan-race") // reads the purged ledger, re-saves

	got, err := findings.LoadFindings(ctx, "scan-race")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	if len(got[0].Evidence.Snippets) == 0 {
		t.Fatal("the second finalizer overwrote the stored findings with evidence-less ones — " +
			"the evidence justifying this finding is permanently gone")
	}
	if got[0].Evidence.Snippets[0] != want.Evidence.Snippets[0] {
		t.Errorf("evidence changed: %v", got[0].Evidence.Snippets)
	}
	if got[0].Key() != want.Key() {
		t.Error("the finding's identity changed")
	}
}

// TestFinalize_OnlyTheCASWinnerPurges: the purge is the one destructive step, so it
// must be gated on the CAS that decides which process is the single finalizer —
// which is why it now runs after that CAS rather than before it.
func TestFinalize_OnlyTheCASWinnerPurges(t *testing.T) {
	store := &purgeCountingStore{TaskStore: taskstore.New()}
	o := testOrch(t, store, fstore.NewInMemory(), newSpyCache(), &platform.Metrics{})
	ctx := context.Background()

	seedFinishedScan(t, store, "scan-once")

	o.finalize(ctx, "scan-once") // wins the CAS (running → completed)
	o.finalize(ctx, "scan-once") // loses it; the scan is already completed

	if store.purges != 1 {
		t.Errorf("the ledger was purged %d times, want 1 (only the CAS winner may purge)", store.purges)
	}
}

type purgeCountingStore struct {
	platform.TaskStore
	purges int
}

func (s *purgeCountingStore) PurgeOutcomeEvidence(ctx context.Context, id domain.ScanID) error {
	s.purges++
	return s.TaskStore.PurgeOutcomeEvidence(ctx, id)
}

// TestFinalize_DoesNotPurgeWhenTheSaveFailed is the ordering rule. The ledger is
// the ONLY other copy of a scan's findings, so purging it before the findings are
// safely stored would destroy them if the store then failed.
func TestFinalize_DoesNotPurgeWhenTheSaveFailed(t *testing.T) {
	store := taskstore.New()
	o := testOrch(t, store, brokenFindingsStore{}, newSpyCache(), &platform.Metrics{})
	ctx := context.Background()

	seedFinishedScan(t, store, "scan-nosave")
	o.finalize(ctx, "scan-nosave")

	s, _ := store.LoadScan(ctx, "scan-nosave")
	if s.State != platform.StateFailed {
		t.Errorf("a scan whose findings could not be stored must fail, got %v", s.State)
	}
	after, _ := store.AllOutcomes(ctx, "scan-nosave")
	if len(after) != 1 || len(after[0].Findings[0].Evidence.Snippets) == 0 {
		t.Fatal("the ledger's evidence was purged even though the save failed — the findings are now gone entirely")
	}
}

// TestGetReport_SameAcrossBackendCombinations. The mem task store keeps the
// findings it was handed; the Mongo one strips them before writing and expects the
// findings store to supply them on read. If GetReport did not unconditionally take
// the findings from the FindingsStore, what a caller got back would depend on which
// pair of backends happened to be wired — and a mem/durable mix would disagree with
// itself. Here both stores hold DIFFERENT findings, and the report must always show
// the findings store's.
func TestGetReport_AlwaysTakesFindingsFromTheFindingsStore(t *testing.T) {
	store := taskstore.New()
	findings := fstore.NewInMemory()
	o := testOrch(t, store, findings, newSpyCache(), &platform.Metrics{})
	ctx := context.Background()

	_ = store.SaveScan(ctx, platform.Scan{ID: "s", State: platform.StateCompleted})

	stale := domain.Finding{CWEs: []domain.CWE{"CWE-1"}, TargetURL: "stale", SourceTool: "stale", Severity: domain.SevLow}
	fresh := domain.Finding{CWEs: []domain.CWE{"CWE-614"}, TargetURL: "fresh", SourceTool: "fresh", Severity: domain.SevHigh}

	// The task store's report carries a stale copy (as the mem impl would).
	if err := store.SaveReport(ctx, "s", report.Report{ScanID: "s", Findings: []domain.Finding{stale}}); err != nil {
		t.Fatal(err)
	}
	// The findings store — the owner — has the real one.
	if err := findings.SaveFindings(ctx, "s", []domain.Finding{fresh}); err != nil {
		t.Fatal(err)
	}

	rep, ok, err := o.GetReport(ctx, "s")
	if err != nil || !ok {
		t.Fatalf("GetReport: ok=%v err=%v", ok, err)
	}
	if len(rep.Findings) != 1 || rep.Findings[0].SourceTool != "fresh" {
		t.Errorf("GetReport served the task store's findings; the FindingsStore is the owner. got %+v", rep.Findings)
	}
}

type brokenFindingsStore struct{}

func (brokenFindingsStore) SaveFindings(context.Context, domain.ScanID, []domain.Finding) error {
	return errors.New("mongo down")
}

func (brokenFindingsStore) LoadFindings(context.Context, domain.ScanID) ([]domain.Finding, error) {
	return nil, errors.New("mongo down")
}

type noopQueue struct{}

func (noopQueue) Enqueue(context.Context, domain.WorkItem) error { return nil }
func (noopQueue) Dequeue(ctx context.Context) (domain.WorkItem, error) {
	<-ctx.Done()
	return domain.WorkItem{}, ctx.Err()
}
func (noopQueue) Drain(context.Context, domain.ScanID) error { return nil }

// toCheckerSlice adapts the probe adapters to the Checker interface (the same
// conversion the composition root does).
func toCheckerSlice(ps []probe.Adapter) []domain.Checker {
	out := make([]domain.Checker, 0, len(ps))
	for _, p := range ps {
		out = append(out, p)
	}
	return out
}
