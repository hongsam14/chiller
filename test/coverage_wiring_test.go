// Package integration verifies that the units cohere end to end with the real
// config: scan's checker CWE-ownership, projected through configs/mappings, plus
// configs/coverage delegations, must cover exactly the 26 KISA items — i.e.
// findings' boot completeness check passes. This is the load-bearing cross-unit
// invariant (R5 totality) exercised with production config, not fixtures.
package integration

import (
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
	fconfig "github.com/hongsam14/chiller/internal/findings/config"
	fcoverage "github.com/hongsam14/chiller/internal/findings/coverage"
	"github.com/hongsam14/chiller/internal/scan/checking/observation"
	"github.com/hongsam14/chiller/internal/scan/checking/probe"
	"github.com/hongsam14/chiller/internal/scan/checking/registry"
)

func TestCoverageWiring_RealConfig_BootPasses(t *testing.T) {
	mt, err := fconfig.LoadMappingFile("../configs/mappings/kisa.yaml")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := fconfig.LoadCoverageSpecFile("../configs/coverage/kisa.yaml")
	if err != nil {
		t.Fatal(err)
	}

	reg := registry.New()
	for _, c := range observation.Checkers(observation.Default()) {
		_ = reg.Register(c)
	}
	for _, p := range probe.Default() {
		_ = reg.Register(p)
	}
	own := fcoverage.CWEOwnership(reg.OwnershipForFindings())

	// Boot completeness: 26 = weakness(ownership∘mapping) ∪ attacker ∪ delegated.
	acc, err := fcoverage.New(own, mt, spec)
	if err != nil {
		t.Fatalf("boot completeness failed — the 26 KISA items are not fully covered by scan ownership + delegations: %v", err)
	}

	// A clean scan where every checker ran ok → all non-delegated items Checked,
	// delegated items Delegated → a valid 26-partition.
	ranOK := map[domain.CheckerID]bool{}
	for id := range own {
		ranOK[id] = true
	}
	cov := acc.Account(fcoverage.ScanOutcome{RanOK: ranOK})
	if err := cov.Validate(spec); err != nil {
		t.Fatalf("coverage not a valid 26-partition: %v", err)
	}
	if len(spec.Items) != 26 {
		t.Fatalf("spec should declare 26 items, got %d", len(spec.Items))
	}

	// Spot-check the two axes.
	if cov["PS-025"].Status != domain.StatusChecked {
		t.Errorf("PS-025 (attacker-axis, subzy) should be Checked, got %v", cov["PS-025"].Status)
	}
	if cov["PS-014"].Status != domain.StatusDelegated || cov["PS-014"].Target != domain.DelegateASM {
		t.Errorf("PS-014 should be Delegated→ASM, got %+v", cov["PS-014"])
	}
	// r21: PS-005/006 promoted from Nuclei delegation to observation-native → now Checked.
	if cov["PS-005"].Status != domain.StatusChecked {
		t.Errorf("PS-005 should be Checked (observation-native r21), got %+v", cov["PS-005"])
	}
	if cov["PS-006"].Status != domain.StatusChecked {
		t.Errorf("PS-006 should be Checked (observation-native r21), got %+v", cov["PS-006"])
	}
	// PS-026 remains the only Nuclei-delegated item alongside PS-014 (ASM).
	if cov["PS-026"].Target != domain.DelegateNuclei {
		t.Errorf("PS-026 should be Delegated→Nuclei, got %+v", cov["PS-026"])
	}
}
