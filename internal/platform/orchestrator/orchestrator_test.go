package orchestrator

import (
	"context"
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/findings/compliance"
	fconfig "github.com/enki-offen/chiller/internal/findings/config"
	"github.com/enki-offen/chiller/internal/platform"
	"github.com/enki-offen/chiller/internal/platform/taskstore"
	"github.com/enki-offen/chiller/internal/scan/checking/observation"
	"github.com/enki-offen/chiller/internal/scan/checking/probe"
	"github.com/enki-offen/chiller/internal/scan/ingest"
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
	if url != 3 {
		t.Errorf("url items = %d, want 3", url)
	}
	if host != 2 {
		t.Errorf("host items = %d, want 2 (distinct hosts a,b)", host)
	}
	// Host-interleave: the first two URL items should target different hosts.
	if items[0].Target.Host == items[1].Target.Host {
		t.Errorf("URL items not host-interleaved: %s then %s", items[0].Target.Host, items[1].Target.Host)
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
