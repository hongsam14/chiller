package store_test

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/findings/store"
)

// runFindingsStoreContract is the implementation-independent behavioral suite for
// store.FindingsStore. The in-memory impl (below) and the durable Mongo impl (in
// the integration test) run the EXACT same suite by passing their constructor, so
// swapping the backend must reproduce identical observable behavior. The mem impl
// also serves as the oracle for the property tests (PBT-05).
func runFindingsStoreContract(t *testing.T, newStore func() store.FindingsStore) {
	t.Helper()
	t.Run("RoundTrip", func(t *testing.T) { testRoundTrip(t, newStore()) })
	t.Run("PreservesOrder", func(t *testing.T) { testPreservesOrder(t, newStore()) })
	t.Run("UnknownScanIsEmpty", func(t *testing.T) { testUnknownScan(t, newStore()) })
	t.Run("SaveIsIdempotent", func(t *testing.T) { testIdempotent(t, newStore()) })
	t.Run("ShorterSaveTruncatesTail", func(t *testing.T) { testTruncatesTail(t, newStore()) })
	t.Run("SaveNeverErasesEvidence", func(t *testing.T) { testNeverErasesEvidence(t, newStore()) })
	t.Run("ScansAreIsolated", func(t *testing.T) { testScanIsolation(t, newStore()) })
	t.Run("DoesNotAliasCallerSlice", func(t *testing.T) { testNoAliasing(t, newStore()) })
	t.Run("PBT_RoundTripInvariants", func(t *testing.T) { testPBTRoundTrip(t, newStore()) })
}

func TestInMemory_Contract(t *testing.T) {
	runFindingsStoreContract(t, func() store.FindingsStore { return store.NewInMemory() })
}

func fnd(tool string, sev domain.Severity, snippets ...string) domain.Finding {
	return domain.Finding{
		CWEs:       []domain.CWE{"CWE-614"},
		TargetURL:  "https://example.test/" + tool,
		SourceTool: tool,
		Severity:   sev,
		Evidence:   domain.Evidence{Snippets: snippets},
	}
}

func testRoundTrip(t *testing.T, s store.FindingsStore) {
	ctx := context.Background()
	want := []domain.Finding{fnd("humble", domain.SevMedium, "Set-Cookie: a=1"), fnd("httpx", domain.SevLow)}
	if err := s.SaveFindings(ctx, "scan-1", want); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadFindings(ctx, "scan-1")
	if err != nil {
		t.Fatal(err)
	}
	assertFindingsEqual(t, got, want)
}

// testPreservesOrder pins the contract that makes report assembly correct: a
// store that returned findings in arbitrary order would still pass a naive
// round-trip test, and would then silently mis-attach framework labels.
func testPreservesOrder(t *testing.T, s store.FindingsStore) {
	ctx := context.Background()
	var want []domain.Finding
	for i := 0; i < 25; i++ {
		want = append(want, fnd(fmt.Sprintf("tool-%02d", i), domain.SevInfo, fmt.Sprintf("snippet %d", i)))
	}
	if err := s.SaveFindings(ctx, "scan-order", want); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadFindings(ctx, "scan-order")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d findings, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].SourceTool != want[i].SourceTool {
			t.Fatalf("order not preserved at %d: got %q, want %q", i, got[i].SourceTool, want[i].SourceTool)
		}
	}
}

func testUnknownScan(t *testing.T, s store.FindingsStore) {
	got, err := s.LoadFindings(context.Background(), "never-saved")
	if err != nil {
		t.Fatalf("an unknown scan must load empty, not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d findings for an unknown scan", len(got))
	}
}

func testIdempotent(t *testing.T, s store.FindingsStore) {
	ctx := context.Background()
	want := []domain.Finding{fnd("humble", domain.SevHigh, "x"), fnd("testssl", domain.SevCritical)}
	for i := 0; i < 3; i++ {
		if err := s.SaveFindings(ctx, "scan-idem", want); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.LoadFindings(ctx, "scan-idem")
	if err != nil {
		t.Fatal(err)
	}
	assertFindingsEqual(t, got, want)
}

// testTruncatesTail is the one the durable impl can fail without noticing: an
// upsert-per-finding write leaves the extra documents of a longer previous save
// behind, so the scan grows phantom findings on a re-run.
func testTruncatesTail(t *testing.T, s store.FindingsStore) {
	ctx := context.Background()
	long := []domain.Finding{fnd("a", domain.SevLow), fnd("b", domain.SevLow), fnd("c", domain.SevLow)}
	if err := s.SaveFindings(ctx, "scan-trunc", long); err != nil {
		t.Fatal(err)
	}
	short := []domain.Finding{fnd("a", domain.SevLow)}
	if err := s.SaveFindings(ctx, "scan-trunc", short); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadFindings(ctx, "scan-trunc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d findings after re-saving a shorter list, want 1 (tail not truncated)", len(got))
	}
	assertFindingsEqual(t, got, short)
}

// testNeverErasesEvidence is the regression for a data-loss bug. finalize purges
// the ledger's evidence once the findings are stored; a finalize that reads the
// ledger AFTER that purge (a re-trigger, or a second finalizer in another pod)
// re-derives the SAME findings with EMPTY evidence. If the store replaced
// faithfully, that second save would overwrite the sealed evidence with nothing —
// and this store is the last copy of it.
func testNeverErasesEvidence(t *testing.T, s store.FindingsStore) {
	ctx := context.Background()
	withEvidence := []domain.Finding{
		fnd("humble", domain.SevHigh, "Set-Cookie: SESSION=s3cret"),
		fnd("httpx", domain.SevLow, "Server: nginx/1.2.3"),
	}
	if err := s.SaveFindings(ctx, "scan-1", withEvidence); err != nil {
		t.Fatal(err)
	}

	// The same findings as a post-purge finalize would see them: identical identity,
	// no evidence.
	stripped := []domain.Finding{fnd("humble", domain.SevHigh), fnd("httpx", domain.SevLow)}
	if err := s.SaveFindings(ctx, "scan-1", stripped); err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadFindings(ctx, "scan-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2", len(got))
	}
	for i := range got {
		if len(got[i].Evidence.Snippets) == 0 {
			t.Fatalf("finding %d lost its evidence to an evidence-less re-save — the only copy is gone", i)
		}
		if got[i].Evidence.Snippets[0] != withEvidence[i].Evidence.Snippets[0] {
			t.Errorf("finding %d evidence changed: %v", i, got[i].Evidence.Snippets)
		}
	}
}

func testScanIsolation(t *testing.T, s store.FindingsStore) {
	ctx := context.Background()
	if err := s.SaveFindings(ctx, "scan-a", []domain.Finding{fnd("a", domain.SevLow)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveFindings(ctx, "scan-b", []domain.Finding{fnd("b", domain.SevHigh), fnd("c", domain.SevHigh)}); err != nil {
		t.Fatal(err)
	}
	a, err := s.LoadFindings(ctx, "scan-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 || a[0].SourceTool != "a" {
		t.Errorf("scan-a leaked: %+v", a)
	}
	b, err := s.LoadFindings(ctx, "scan-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 2 {
		t.Errorf("scan-b got %d findings, want 2", len(b))
	}
}

func testNoAliasing(t *testing.T, s store.FindingsStore) {
	ctx := context.Background()
	in := []domain.Finding{fnd("humble", domain.SevLow, "original")}
	if err := s.SaveFindings(ctx, "scan-alias", in); err != nil {
		t.Fatal(err)
	}
	in[0].SourceTool = "mutated"
	in[0].Evidence.Snippets[0] = "mutated"

	got, err := s.LoadFindings(ctx, "scan-alias")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].SourceTool != "humble" {
		t.Error("store aliased the caller's slice")
	}
	if len(got[0].Evidence.Snippets) > 0 && got[0].Evidence.Snippets[0] != "original" {
		t.Error("store aliased the caller's evidence snippets")
	}
}

// testPBTRoundTrip is the round-trip property (PBT-02) over generated findings:
// whatever we save, we load back identically — same order, same normalized CWEs,
// same bounded evidence. The generator deliberately produces CWE lists that are
// unsorted, duplicated and sentinel-mixed, and snippet lists that exceed
// MaxEvidenceSnippets, so the invariants are exercised rather than assumed.
func testPBTRoundTrip(t *testing.T, s store.FindingsStore) {
	ctx := context.Background()
	rng := rand.New(rand.NewSource(20260713))

	for iter := 0; iter < 60; iter++ {
		n := rng.Intn(12)
		findings := make([]domain.Finding, 0, n)
		for i := 0; i < n; i++ {
			findings = append(findings, genFinding(rng, i))
		}
		scanID := domain.ScanID(fmt.Sprintf("pbt-%d", iter))
		if err := s.SaveFindings(ctx, scanID, findings); err != nil {
			t.Fatalf("seed 20260713 iter %d: save: %v", iter, err)
		}
		got, err := s.LoadFindings(ctx, scanID)
		if err != nil {
			t.Fatalf("seed 20260713 iter %d: load: %v", iter, err)
		}
		if len(got) != len(findings) {
			t.Fatalf("seed 20260713 iter %d: got %d findings, want %d", iter, len(got), len(findings))
		}
		for i := range findings {
			if got[i].Key() != findings[i].Key() {
				t.Fatalf("seed 20260713 iter %d: finding %d changed identity across the round trip", iter, i)
			}
			if got[i].Severity != findings[i].Severity {
				t.Fatalf("seed 20260713 iter %d: finding %d severity %v != %v", iter, i, got[i].Severity, findings[i].Severity)
			}
			if len(got[i].Evidence.Snippets) > domain.MaxEvidenceSnippets {
				t.Fatalf("seed 20260713 iter %d: finding %d came back with %d snippets, over the %d bound",
					iter, i, len(got[i].Evidence.Snippets), domain.MaxEvidenceSnippets)
			}
			if !equalSnippets(got[i].Evidence.Snippets, findings[i].Evidence.Snippets) {
				t.Fatalf("seed 20260713 iter %d: finding %d evidence changed across the round trip", iter, i)
			}
		}
	}
}

var genCWEs = []domain.CWE{"CWE-614", "CWE-1004", "CWE-693", "CWE-200", "CWE-16"}
var genSevs = []domain.Severity{domain.SevInfo, domain.SevLow, domain.SevMedium, domain.SevHigh, domain.SevCritical}

func genFinding(rng *rand.Rand, i int) domain.Finding {
	cwes := []domain.CWE{genCWEs[rng.Intn(len(genCWEs))]}
	for rng.Intn(3) == 0 {
		cwes = append(cwes, genCWEs[rng.Intn(len(genCWEs))]) // duplicates and unsorted, on purpose
	}
	// Evidence is bounded by the producer (R9); generate up to the bound.
	nSnip := rng.Intn(domain.MaxEvidenceSnippets + 1)
	snips := make([]string, nSnip)
	for j := range snips {
		snips[j] = strings.Repeat("e", rng.Intn(40)) + fmt.Sprintf("#%d", j)
	}
	return domain.Finding{
		CWEs:       domain.NormalizeCWEs(cwes),
		TargetURL:  fmt.Sprintf("https://host-%d.test/p?q=%d", rng.Intn(5), i),
		SourceTool: fmt.Sprintf("tool-%d", rng.Intn(4)),
		Severity:   genSevs[rng.Intn(len(genSevs))],
		Evidence:   domain.Evidence{Snippets: snips},
	}
}

func equalSnippets(a, b []string) bool {
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

func assertFindingsEqual(t *testing.T, got, want []domain.Finding) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d findings, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Key() != want[i].Key() {
			t.Errorf("finding %d: identity %q != %q", i, got[i].Key(), want[i].Key())
		}
		if got[i].Severity != want[i].Severity {
			t.Errorf("finding %d: severity %v != %v", i, got[i].Severity, want[i].Severity)
		}
		if !equalSnippets(got[i].Evidence.Snippets, want[i].Evidence.Snippets) {
			t.Errorf("finding %d: evidence %v != %v", i, got[i].Evidence.Snippets, want[i].Evidence.Snippets)
		}
	}
}
