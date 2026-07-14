package exec

import (
	"context"
	"errors"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/scan/fetch"
	"github.com/hongsam14/chiller/internal/scan/safety"
)

// fakeChecker is a controllable domain.Checker.
type fakeChecker struct {
	id    domain.CheckerID
	scope domain.ScopeKind
	res   domain.CheckResult
	err   error
}

func (f fakeChecker) ID() domain.CheckerID    { return f.id }
func (f fakeChecker) Scope() domain.ScopeKind { return f.scope }
func (f fakeChecker) Check(domain.ScanContext) (domain.CheckResult, error) {
	return f.res, f.err
}

// fakeFetcher returns a canned HAR (or an error).
type fakeFetcher struct {
	entry *domain.HAREntry
	err   error
}

func (f fakeFetcher) Fetch(context.Context, domain.ScanID, domain.Target) (*domain.HAREntry, error) {
	return f.entry, f.err
}

type noDisc struct{}

func (noDisc) Discover(domain.Target, *domain.HAREntry) []domain.Target { return nil }

// openGate approves every host (unblocks host-scoped runs in the general tests).
type openGate struct{}

func (openGate) Authorize(domain.Target) error { return nil }

func TestRun_AggregatesFindingsFailedNA(t *testing.T) {
	checkers := []domain.Checker{
		fakeChecker{id: "a", scope: domain.URLScoped, res: domain.CheckResult{Findings: []domain.Finding{{CWEs: []domain.CWE{"CWE-1"}, TargetURL: "u", SourceTool: "a", Severity: domain.SevLow}}}},
		fakeChecker{id: "b", scope: domain.URLScoped, err: errors.New("boom")},                                       // tool-failed
		fakeChecker{id: "c", scope: domain.URLScoped, res: domain.CheckResult{NotApplicable: []domain.CWE{"CWE-3"}}}, // N/A
	}
	e := New(checkers, fakeFetcher{entry: &domain.HAREntry{Status: 200}}, noDisc{}, openGate{})
	out := e.Run(context.Background(), domain.WorkItem{ID: "w1", ScanID: "s1", Scope: domain.URLScoped, Target: domain.Target{URL: "u"}}, fetch.NewInMemoryCache())

	if len(out.Findings) != 1 {
		t.Errorf("findings = %d, want 1", len(out.Findings))
	}
	if len(out.Failed) != 1 || out.Failed[0] != "b" {
		t.Errorf("failed = %v, want [b]", out.Failed)
	}
	if cwes := out.NotApplicable["c"]; len(cwes) != 1 || cwes[0] != "CWE-3" {
		t.Errorf("N/A = %v, want c:[CWE-3]", out.NotApplicable)
	}
}

func TestRun_FetchFailure_AllUrlScopedToolFailed(t *testing.T) {
	checkers := []domain.Checker{
		fakeChecker{id: "a", scope: domain.URLScoped},
		fakeChecker{id: "b", scope: domain.URLScoped},
	}
	e := New(checkers, fakeFetcher{err: errors.New("dns")}, noDisc{}, openGate{})
	out := e.Run(context.Background(), domain.WorkItem{ID: "w", ScanID: "s", Scope: domain.URLScoped}, fetch.NewInMemoryCache())
	if len(out.Failed) != 2 || len(out.Findings) != 0 {
		t.Errorf("fetch failure ⇒ all url-scoped tool-failed; got failed=%v findings=%d", out.Failed, len(out.Findings))
	}
}

func TestRun_HostScoped_NoFetch(t *testing.T) {
	// host-scoped checker runs without a fetch (Response nil).
	checkers := []domain.Checker{fakeChecker{id: "testssl", scope: domain.HostScoped, res: domain.CheckResult{}}}
	// a fetcher that would error if called — proving host-scoped doesn't fetch
	e := New(checkers, fakeFetcher{err: errors.New("should not be called")}, noDisc{}, openGate{})
	out := e.Run(context.Background(), domain.WorkItem{ID: "h", ScanID: "s", Scope: domain.HostScoped, Target: domain.Target{Host: "a.test"}}, fetch.NewInMemoryCache())
	if len(out.Failed) != 0 {
		t.Errorf("host-scoped run should not fetch or fail; got %v", out.Failed)
	}
}

// H2 regression: a host-scoped probe must not run against an unapproved host —
// the gate fails it closed even though there is no URL fetch on this path.
func TestRun_HostScoped_UnapprovedHost_FailClosed(t *testing.T) {
	checkers := []domain.Checker{fakeChecker{id: "testssl", scope: domain.HostScoped}}
	gate := safety.NewApprovalGate([]string{"approved.test"}, nil) // b.test NOT approved
	e := New(checkers, fakeFetcher{}, noDisc{}, gate)
	out := e.Run(context.Background(), domain.WorkItem{ID: "h", ScanID: "s", Scope: domain.HostScoped, Target: domain.Target{Host: "b.test"}}, fetch.NewInMemoryCache())
	if len(out.Failed) != 1 || out.Failed[0] != "testssl" {
		t.Errorf("unapproved host-scoped probe must be failed-closed, got failed=%v", out.Failed)
	}
}
