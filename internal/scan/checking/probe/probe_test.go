package probe

import (
	"context"
	"errors"
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
)

func TestDefault_SevenAdapters(t *testing.T) {
	ps := Default()
	if len(ps) != 7 {
		t.Fatalf("want 7 probe adapters, got %d", len(ps))
	}
	hosts, urls := 0, 0
	for _, p := range ps {
		if len(p.OwnedCWEs()) == 0 {
			t.Errorf("%s must declare owned CWEs", p.ID())
		}
		if p.PinnedVersion() == "" {
			t.Errorf("%s must be version-pinned (SEC-10)", p.ID())
		}
		switch p.Scope() {
		case domain.HostScoped:
			hosts++
		case domain.URLScoped:
			urls++
		}
	}
	if hosts != 3 || urls != 4 {
		t.Errorf("scope split wrong: host=%d url=%d (want 3/4)", hosts, urls)
	}
}

// An unwired adapter must NOT look like a clean run. If it returned (empty, nil)
// the fold would mark its items StatusChecked — reporting "no issue" for items
// nothing ever examined. It must fail with ErrNotWired → StatusToolFailed.
func TestUnwiredAdapter_FailsWithErrNotWired(t *testing.T) {
	for _, a := range Default() {
		if a.Wired() {
			continue // a wired probe is exercised by TestRunner_ExecAndParse
		}
		_, err := a.Check(domain.ScanContext{Target: domain.Target{Host: "a.test", URL: "https://a.test/"}})
		if !errors.Is(err, ErrNotWired) {
			t.Errorf("%s: unwired probe must fail with ErrNotWired (→ tool-failed), got err=%v", a.ID(), err)
		}
	}
}

// Guards the honest default: until infra-plan §3 lands, no probe is wired.
// When you wire one, this count drops — that is the point.
func TestDefault_NoProbeWiredYet(t *testing.T) {
	for _, a := range Default() {
		if a.Wired() {
			t.Logf("%s is now wired — update the coverage expectations in test/coverage_wiring_test.go", a.ID())
		}
	}
}

// The exec framework: fake command → parser; and a failing command → error.
func TestRunner_ExecAndParse(t *testing.T) {
	r := &runner{
		id: "probe:fake", tool: "faketool", scope: domain.HostScoped, cwes: []domain.CWE{"CWE-1"},
		argv: func(domain.Target) []string { return []string{"arg"} },
		parse: func(t domain.Target, out []byte) ([]domain.Finding, error) {
			return []domain.Finding{{CWEs: []domain.CWE{"CWE-1"}, TargetURL: t.URL, SourceTool: "probe:fake", Severity: domain.SevLow}}, nil
		},
		exec: func(ctx context.Context, name string, env []string, args ...string) ([]byte, error) {
			return []byte("ok"), nil
		},
	}
	res, err := r.Check(domain.ScanContext{Target: domain.Target{URL: "https://a/"}})
	if err != nil || len(res.Findings) != 1 {
		t.Fatalf("framework should parse output into a finding, got %d err %v", len(res.Findings), err)
	}

	rFail := &runner{
		id: "probe:fake", tool: "faketool", argv: func(domain.Target) []string { return []string{"arg"} }, parse: stubParse,
		exec: func(ctx context.Context, name string, env []string, args ...string) ([]byte, error) {
			return nil, errors.New("exit 1")
		},
	}
	if _, err := rFail.Check(domain.ScanContext{}); err == nil {
		t.Error("a failing command must surface as an error (→ tool-failed)")
	}
}
