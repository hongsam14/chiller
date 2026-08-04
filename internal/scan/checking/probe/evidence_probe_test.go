package probe

import (
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

// r20 honesty property (FR-E7): probe evidence is best-effort — it records the tool
// (rule) and the raw line (snippet) but NEVER fabricates a match location or a
// request/response context it did not observe.
func TestProbeEvidence_NoFabrication(t *testing.T) {
	f := pFinding("CWE-319", "humble", "https://h/", "HSTS header not set")
	if len(f.Evidence.Items) != 1 {
		t.Fatalf("want 1 evidence item, got %d", len(f.Evidence.Items))
	}
	it := f.Evidence.Items[0]
	if it.Snippet == "" {
		t.Error("snippet (raw line) must be recorded")
	}
	if it.Rule != "humble" {
		t.Errorf("rule must attribute the tool, got %q", it.Rule)
	}
	if it.Hypothesis == "" {
		t.Error("hypothesis should be set from the owned CWE")
	}
	// The honesty invariant: no fabricated location/context.
	if it.Location != (domain.Location{}) {
		t.Errorf("probe must not fabricate a match location, got %#v", it.Location)
	}
	if f.Evidence.Request != nil || f.Evidence.Response != nil {
		t.Errorf("probe has no HAR — request/response must be nil, got req=%#v resp=%#v",
			f.Evidence.Request, f.Evidence.Response)
	}
}
