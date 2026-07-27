package orchestrator

import (
	"context"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/findings/compliance"
	fconfig "github.com/hongsam14/chiller/internal/findings/config"
	"github.com/hongsam14/chiller/internal/findings/coverage"
	"github.com/hongsam14/chiller/internal/platform"
	"github.com/hongsam14/chiller/internal/platform/taskstore"
	"github.com/hongsam14/chiller/internal/scan/checking/observation"
	"github.com/hongsam14/chiller/internal/scan/checking/probe"
	"github.com/hongsam14/chiller/internal/scan/ingest"
)

const (
	mappingPath  = "../../../configs/mappings/kisa.yaml"
	coveragePath = "../../../configs/coverage/kisa.yaml"
)

// fakeChecker is a URL-scoped checker with a declared CWE for fold tests.
type fakeChecker struct {
	id    domain.CheckerID
	cwe   domain.CWE
	scope domain.ScopeKind
}

func (f fakeChecker) Check(domain.ScanContext) (domain.CheckResult, error) {
	return domain.CheckResult{}, nil
}
func (f fakeChecker) Scope() domain.ScopeKind { return f.scope }
func (f fakeChecker) ID() domain.CheckerID    { return f.id }
func (f fakeChecker) OwnedCWEs() []domain.CWE { return []domain.CWE{f.cwe} }

func TestFanOut_URLAndHostCounts_Interleaved(t *testing.T) {
	set := ingest.URLSet{URLs: []domain.Target{
		{URL: "http://a/1", Host: "a", Scheme: "http"},
		{URL: "http://a/2", Host: "a", Scheme: "http"},
		{URL: "http://b/1", Host: "b", Scheme: "http"},
	}}
	items := fanOut("s", set, true, true)

	var url, host int
	for _, it := range items {
		switch it.Scope {
		case domain.URLScoped:
			url++
		case domain.HostScoped:
			host++
		}
	}
	// URL items = 3 submitted + a well-known sweep per distinct host (a, b), each
	// URL-scoped (r10). Host items = one per distinct host.
	wantURL := 3 + 2*len(observation.WellKnownPaths())
	if url != wantURL {
		t.Errorf("url items = %d, want %d (3 submitted + 2 hosts × %d well-known)", url, wantURL, len(observation.WellKnownPaths()))
	}
	if host != 2 {
		t.Errorf("host items = %d, want 2 (distinct hosts a,b)", host)
	}
	// Host-interleave (PR-3a) must hold across the WHOLE URL-scoped set — submitted
	// AND synthesized well-known items. A regression that appends the well-known
	// sweep host-major (all of host a, then all of host b) would leave a long run
	// of consecutive same-host items; assert no run exceeds 1 across the interleaved
	// URL items (a stronger check than only inspecting items[0]/items[1]).
	run, maxRun, prev := 0, 0, ""
	for _, it := range items {
		if it.Scope != domain.URLScoped {
			continue
		}
		if it.Target.Host == prev {
			run++
		} else {
			run = 1
			prev = it.Target.Host
		}
		if run > maxRun {
			maxRun = run
		}
	}
	// hosts a (2 submitted + N well-known) and b (1 + N) round-robin cleanly, so no
	// two consecutive URL items share a host until at most the tail remainder.
	if maxRun > 2 {
		t.Errorf("URL items not host-interleaved: max same-host run = %d (want ≤2); well-known sweep appended host-major?", maxRun)
	}
}

// TestFanOut_WellKnown_Disabled: the off-switch suppresses the whole sweep, and
// TestFanOut_WellKnown_HostCap: the per-scan host cap bounds it (r10, finding 9 —
// the sweep sits outside MaxURLs so it must carry its own ceiling).
func TestFanOut_WellKnown_Disabled(t *testing.T) {
	set := ingest.URLSet{
		URLs:    []domain.Target{{URL: "http://a/1", Host: "a", Scheme: "http"}},
		Options: ingest.ScanOptions{DisableWellKnown: true},
	}
	items := fanOut("s", set, true, false)
	for _, it := range items {
		if it.WellKnown {
			t.Errorf("DisableWellKnown set, but a well-known item was synthesized: %+v", it.Target)
		}
	}
	if len(items) != 1 {
		t.Errorf("url items = %d, want 1 (only the submitted URL, no sweep)", len(items))
	}
}

func TestFanOut_WellKnown_HostCap(t *testing.T) {
	set := ingest.URLSet{
		URLs: []domain.Target{
			{URL: "http://a/1", Host: "a", Scheme: "http"},
			{URL: "http://b/1", Host: "b", Scheme: "http"},
			{URL: "http://c/1", Host: "c", Scheme: "http"},
		},
		Options: ingest.ScanOptions{MaxWellKnownHosts: 2}, // cap below the 3 distinct hosts
	}
	items := fanOut("s", set, true, false)
	swept := map[string]bool{}
	for _, it := range items {
		if it.WellKnown {
			swept[it.Target.Host] = true
		}
	}
	if len(swept) != 2 {
		t.Errorf("well-known swept %d hosts, want 2 (MaxWellKnownHosts cap)", len(swept))
	}
}

// TestFanOut_WellKnownSynthesis: for each distinct host, one URL-scoped item per
// well-known path is synthesized (r10, PR-W1) — deduped per host, carrying the
// well-known path in Target.URL so the owning signature keys off it.
func TestFanOut_WellKnownSynthesis(t *testing.T) {
	set := ingest.URLSet{URLs: []domain.Target{
		{URL: "http://a/1", Host: "a", Scheme: "http"},
		{URL: "http://a/2", Host: "a", Scheme: "http"}, // same host → one sweep
		{URL: "https://b/x", Host: "b", Scheme: "https"},
	}}
	items := fanOut("s", set, true, false) // haveURL only

	// Collect synthesized well-known items (URL path matches a well-known path).
	wk := map[string]map[string]bool{} // host → set of well-known paths seen
	for _, it := range items {
		for _, p := range observation.WellKnownPaths() {
			if it.Target.URL == it.Target.Scheme+"://"+it.Target.Host+p {
				if wk[it.Target.Host] == nil {
					wk[it.Target.Host] = map[string]bool{}
				}
				wk[it.Target.Host][p] = true
			}
		}
	}
	for _, h := range []string{"a", "b"} {
		if len(wk[h]) != len(observation.WellKnownPaths()) {
			t.Errorf("host %s: got %d well-known items, want %d (one sweep per host)", h, len(wk[h]), len(observation.WellKnownPaths()))
		}
	}
	// Every synthesized item is URL-scoped and uses the host's own scheme.
	for _, it := range items {
		if it.Target.Host == "b" && it.Target.Scheme != "https" {
			t.Errorf("host b well-known item lost its scheme: %+v", it.Target)
		}
	}
}

// TestFanOut_WellKnown_MaxURLsIndependent: synthesis happens after ingest, so the
// well-known items are NOT bounded by the user's MaxURLs (Q6=A/PR-W3). fanOut
// receives an already-validated URLSet, so this asserts the structural property:
// the count of synthesized items depends only on distinct hosts, not on MaxURLs.
func TestFanOut_WellKnown_MaxURLsIndependent(t *testing.T) {
	set := ingest.URLSet{
		URLs:    []domain.Target{{URL: "http://a/1", Host: "a", Scheme: "http"}},
		Options: ingest.ScanOptions{MaxURLs: 1}, // user ceiling already applied upstream
	}
	items := fanOut("s", set, true, false)
	urlItems := 0
	for _, it := range items {
		if it.Scope == domain.URLScoped {
			urlItems++
		}
	}
	// 1 submitted + a full well-known sweep — the MaxURLs=1 does not clip the sweep.
	if want := 1 + len(observation.WellKnownPaths()); urlItems != want {
		t.Errorf("url items = %d, want %d — well-known must not be clipped by MaxURLs", urlItems, want)
	}
}

// PR-5 fold: applicable-ok anywhere → RanOK (Checked); na-only → NotApplicable;
// fail-only → neither (ToolFailed via absence of RanOK).
func TestFoldOutcomes_PR5Rules(t *testing.T) {
	mt, err := fconfig.LoadMappingFile(mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	// c1 owns a CWE and applies ok; c2 owns a CWE and is N/A everywhere; c3 fails.
	c1 := fakeChecker{id: "fake:ok", cwe: "CWE-311", scope: domain.URLScoped}   // → PS-003
	c2 := fakeChecker{id: "fake:na", cwe: "CWE-527", scope: domain.URLScoped}   // → PS-010
	c3 := fakeChecker{id: "fake:fail", cwe: "CWE-651", scope: domain.URLScoped} // → PS-008

	store := taskstore.New()
	ctx := context.Background()
	_ = store.SaveScan(ctx, platform.Scan{ID: "s", State: platform.StateRunning})
	items := []domain.WorkItem{{ID: "s#0", ScanID: "s", Scope: domain.URLScoped}}
	_ = store.AppendWorkItems(ctx, "s", items)

	o := New(Config{
		Store:    store,
		Mapper:   compliance.New(mt),
		Checkers: []domain.Checker{c1, c2, c3},
	})

	outcomes := []domain.WorkItemOutcome{{
		ID:            "s#0",
		Failed:        []domain.CheckerID{"fake:fail"},
		NotApplicable: map[domain.CheckerID][]domain.CWE{"fake:na": {"CWE-527"}},
	}}

	so := o.foldOutcomes("s", ctx, outcomes)

	if !so.RanOK["fake:ok"] {
		t.Errorf("applicable-ok checker should be RanOK")
	}
	if so.RanOK["fake:na"] || so.RanOK["fake:fail"] {
		t.Errorf("na/fail checkers must not be RanOK: %+v", so.RanOK)
	}
	if !so.NotApplicable["fake:na"]["PS-010"] {
		t.Errorf("na-only checker should be NotApplicable for its PS-ID, got %+v", so.NotApplicable)
	}
	if !so.Failed["fake:fail"] {
		t.Errorf("fail-only checker should be Failed, got %+v", so.Failed)
	}
}

func TestBootGate_RealConfig_Passes(t *testing.T) {
	mt, err := fconfig.LoadMappingFile(mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := fconfig.LoadCoverageSpecFile(coveragePath)
	if err != nil {
		t.Fatal(err)
	}
	var checkers []domain.Checker
	checkers = append(checkers, observation.Checkers(observation.Default())...)
	for _, p := range probe.Default() {
		checkers = append(checkers, p)
	}
	acc, mapper, err := BootGate(checkers, mt, spec)
	if err != nil {
		t.Fatalf("boot gate should pass with real config: %v", err)
	}
	if acc == nil || mapper == nil {
		t.Fatal("boot gate returned nil accountant/mapper")
	}
}

// TestBootGate_FSEC_Passes wires the SECOND framework (FSEC) end-to-end through
// the real boot gate: the full passive checker set + the fsec mapping/coverage.
// It must boot (P5 totality holds via the empty-projection rows) and produce the
// honest partition — 040/051 owned+accounted, the five active items delegated.
func TestBootGate_FSEC_Passes(t *testing.T) {
	const (
		fsecMap = "../../../configs/mappings/fsec.yaml"
		fsecCov = "../../../configs/coverage/fsec.yaml"
	)
	mt, err := fconfig.LoadMappingFile(fsecMap)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := fconfig.LoadCoverageSpecFile(fsecCov)
	if err != nil {
		t.Fatal(err)
	}
	if spec.FrameworkOrDefault() != domain.FrameworkFSEC {
		t.Fatalf("fsec coverage framework = %q, want FSEC", spec.FrameworkOrDefault())
	}
	var checkers []domain.Checker
	checkers = append(checkers, observation.Checkers(observation.Default())...)
	for _, p := range probe.Default() {
		checkers = append(checkers, p)
	}
	acc, _, err := BootGate(checkers, mt, spec)
	if err != nil {
		t.Fatalf("FSEC boot gate should pass with real config + full checkers: %v", err)
	}
	if acc.Framework() != domain.FrameworkFSEC {
		t.Errorf("accountant framework = %q, want FSEC", acc.Framework())
	}
	// With every owner ranking OK, the two passive items are Checked and the five
	// active items are Delegated — a valid 7-item partition.
	so := coverage.ScanOutcome{RanOK: map[domain.CheckerID]bool{}}
	for _, c := range checkers {
		so.RanOK[c.ID()] = true
	}
	cov := acc.Account(so)
	if err := cov.Validate(spec); err != nil {
		t.Fatalf("FSEC coverage is not a valid partition: %v", err)
	}
	var checked, delegated int
	for _, rec := range cov {
		switch rec.Status {
		case domain.StatusChecked:
			checked++
		case domain.StatusDelegated:
			delegated++
		}
	}
	if checked != 2 || delegated != 5 {
		t.Errorf("FSEC partition = checked %d / delegated %d, want 2 / 5 (040,051 owned; 5 active delegated)", checked, delegated)
	}
}

func TestBootGate_MissingCoverage_Refuses(t *testing.T) {
	mt, err := fconfig.LoadMappingFile(mappingPath)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := fconfig.LoadCoverageSpecFile(coveragePath)
	if err != nil {
		t.Fatal(err)
	}
	// Drop one checker so an item becomes uncovered → boot must refuse.
	all := observation.Default()
	reduced := observation.Checkers(all[1:]) // omit the first signature
	for _, p := range probe.Default() {
		reduced = append(reduced, p)
	}
	if _, _, err := BootGate(reduced, mt, spec); err == nil {
		t.Fatal("boot gate must refuse when an item is uncovered")
	}
}
