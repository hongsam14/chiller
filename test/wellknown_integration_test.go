//go:build integration

// End-to-end proof for the r10 well-known-path sweep: a real orchestrator scan of
// a single homepage URL must, via the synthesized well-known items, surface the
// observation findings whose artifacts live at /robots.txt, /.env, /.git/*,
// /crossdomain.xml, a backup name, and /admin — items that were false-clean/N-A
// before this increment. Opt-in via WELLKNOWN_FIXTURE_URL, pointed at the
// committed test/fixtures/vuln-target/ `vulnweb` service (run this container on
// that fixture's network). Use the container's IP, not the compose service name:
// the scan Fetcher (projectdiscovery retryablehttp) resolves via its own DNS
// resolver, which does not use Docker's embedded DNS — a real DNS name resolves
// fine, a compose service name does not. Skips otherwise, keeping base CI green.
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/platform"
)

func TestIntegration_WellKnown_Fixture(t *testing.T) {
	base := os.Getenv("WELLKNOWN_FIXTURE_URL")
	if base == "" {
		t.Skip("WELLKNOWN_FIXTURE_URL unset — set it to the vuln-target vulnweb URL (e.g. http://vulnweb/)")
	}
	h := newHarness(t, harnessOpts{})
	defer h.stop()

	host := hostOf(t, base)
	id := h.submit(t, []string{base}, []string{host})
	h.waitState(t, id, platform.StateCompleted, 30*time.Second)

	rep, ready, err := h.orch.GetReport(context.Background(), id)
	if err != nil || !ready {
		t.Fatalf("report not ready: ready=%v err=%v", ready, err)
	}

	got := map[domain.CWE]bool{}
	for _, f := range rep.Findings {
		for _, c := range f.CWEs {
			got[c] = true
		}
	}
	// The owning CWE of each well-known item must now appear — proof the sweep
	// delivered each artifact to its signature.
	want := map[domain.CWE]string{
		"CWE-651": "robots.txt sensitive path (PS-008)",
		"CWE-538": ".env exposure (PS-011)",
		"CWE-527": ".git exposure (PS-010)",
		"CWE-942": "crossdomain wildcard (PS-022)",
		"CWE-530": "backup file (PS-012)",
		"CWE-306": "admin reachable (PS-013)",
	}
	for cwe, desc := range want {
		if !got[cwe] {
			t.Errorf("well-known sweep did not surface %s — %s", cwe, desc)
		}
	}
	if err := rep.Coverage.Validate(h.spec); err != nil {
		t.Fatalf("coverage is not a valid 26-partition: %v", err)
	}
}
