package registry

import (
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/scan/checking/observation"
	"github.com/hongsam14/chiller/internal/scan/checking/probe"
)

func TestRegistry_OwnershipForFindings(t *testing.T) {
	r := New()
	for _, c := range observation.Checkers(observation.Default()) {
		_ = r.Register(c)
	}
	for _, p := range probe.Default() {
		_ = r.Register(p)
	}
	own := r.OwnershipForFindings()
	// 13 observation signature-checkers (10 + 3 cookie-flag sigs moved from humble,
	// #0d) + 7 probe adapters, each declaring CWEs.
	if len(own) != 20 {
		t.Errorf("ownership entries = %d, want 20", len(own))
	}
	if got := own["observation:CWE-615"]; len(got) != 1 || got[0] != "CWE-615" {
		t.Errorf("observation:CWE-615 ownership = %v", got)
	}
	if got := own["probe:subzy"]; len(got) != 1 || got[0] != domain.NoCanonicalCWE {
		t.Errorf("subzy should own no-canonical-cwe, got %v", got)
	}
}

func TestRegistry_RegisterIdempotent(t *testing.T) {
	r := New()
	c := observation.AsChecker(observation.Default()[0])
	_ = r.Register(c)
	_ = r.Register(c)
	if len(r.List()) != 1 {
		t.Errorf("duplicate register should be idempotent, got %d", len(r.List()))
	}
}
