package coverage

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/findings/config"
)

// A tiny world: 4 KISA items. PS-1/PS-2 via CWE ownership, PS-3 via attacker
// axis, PS-4 delegated.
const miniMapping = `
version: "t"
map:
  CWE-A: { KISA: [PS-1] }
  CWE-B: { KISA: [PS-2] }
  no-canonical-cwe: { KISA: [] }
attackTechnique:
  PS-3:
    owner: "probe:subzy"
    labels: { ATTACK: [T1584.001] }
`

func miniTable(t *testing.T) *config.MappingTable {
	t.Helper()
	mt, err := config.LoadMapping(strings.NewReader(miniMapping))
	if err != nil {
		t.Fatal(err)
	}
	return mt
}

func miniSpec() domain.CoverageSpec {
	return domain.CoverageSpec{
		Items:       []domain.PSID{"PS-1", "PS-2", "PS-3", "PS-4"},
		Delegations: map[domain.PSID]domain.DelegateTarget{"PS-4": domain.DelegateNuclei},
	}
}

func miniOwnership() CWEOwnership {
	return CWEOwnership{
		"observation:CWE-A": {"CWE-A"},
		"observation:CWE-B": {"CWE-B"},
		// PS-3 owner (subzy) comes from the attacker axis in the mapping.
	}
}

func TestNew_BootCompletenessOK(t *testing.T) {
	if _, err := New(miniOwnership(), miniTable(t), miniSpec()); err != nil {
		t.Fatalf("expected valid boot, got %v", err)
	}
}

func TestNew_GapFailsBoot(t *testing.T) {
	// Drop the PS-2 owner → PS-2 neither owned nor delegated → boot must fail.
	own := CWEOwnership{"observation:CWE-A": {"CWE-A"}}
	if _, err := New(own, miniTable(t), miniSpec()); err == nil {
		t.Error("expected boot failure for uncovered PS-2 (R5 totality)")
	}
}

func TestNew_UnmappedCWEFailsBoot(t *testing.T) {
	own := CWEOwnership{"observation:CWE-A": {"CWE-A"}, "observation:CWE-B": {"CWE-B"}, "x": {"CWE-UNKNOWN"}}
	if _, err := New(own, miniTable(t), miniSpec()); err == nil {
		t.Error("expected boot failure for unmapped owned CWE (P5)")
	}
}

func TestAccount_ValidPartition(t *testing.T) {
	acc, err := New(miniOwnership(), miniTable(t), miniSpec())
	if err != nil {
		t.Fatal(err)
	}
	cov := acc.Account(ScanOutcome{
		RanOK: map[domain.CheckerID]bool{"observation:CWE-A": true, "observation:CWE-B": true, "probe:subzy": true},
	})
	if err := cov.Validate(acc.Spec()); err != nil {
		t.Fatalf("coverage must validate against spec (P4): %v", err)
	}
	if cov["PS-1"].Status != domain.StatusChecked {
		t.Errorf("PS-1 should be Checked, got %v", cov["PS-1"].Status)
	}
	if cov["PS-4"].Status != domain.StatusDelegated || cov["PS-4"].Target != domain.DelegateNuclei {
		t.Errorf("PS-4 should be Delegated→Nuclei, got %+v", cov["PS-4"])
	}
}

func TestAccount_ToolFailed(t *testing.T) {
	acc, _ := New(miniOwnership(), miniTable(t), miniSpec())
	cov := acc.Account(ScanOutcome{
		RanOK:  map[domain.CheckerID]bool{"observation:CWE-B": true, "probe:subzy": true},
		Failed: map[domain.CheckerID]bool{"observation:CWE-A": true}, // PS-1 owner failed
	})
	if cov["PS-1"].Status != domain.StatusToolFailed {
		t.Errorf("PS-1 owner failed ⇒ ToolFailed, got %v", cov["PS-1"].Status)
	}
	if err := cov.Validate(acc.Spec()); err != nil {
		t.Fatalf("still a valid partition: %v", err)
	}
}

func TestAccount_NotApplicable(t *testing.T) {
	acc, _ := New(miniOwnership(), miniTable(t), miniSpec())
	cov := acc.Account(ScanOutcome{
		RanOK:         map[domain.CheckerID]bool{"observation:CWE-A": true, "observation:CWE-B": true, "probe:subzy": true},
		NotApplicable: map[domain.CheckerID]map[domain.PSID]bool{"observation:CWE-A": {"PS-1": true}},
	})
	if cov["PS-1"].Status != domain.StatusNotApplicable {
		t.Errorf("PS-1 signaled N/A ⇒ NotApplicable, got %v", cov["PS-1"].Status)
	}
}

// P6: adding an OWASP section to the mapping must NOT change coverage.
func TestAccount_Property_ProjectionDoesNotCreateCoverage(t *testing.T) {
	spec, own := miniSpec(), miniOwnership()
	base, _ := New(own, miniTable(t), spec)

	withOWASP := strings.Replace(miniMapping,
		"  CWE-A: { KISA: [PS-1] }",
		"  CWE-A: { KISA: [PS-1], OWASP: [A05] }", 1)
	mt2, err := config.LoadMapping(strings.NewReader(withOWASP))
	if err != nil {
		t.Fatal(err)
	}
	added, err := New(own, mt2, spec)
	if err != nil {
		t.Fatal(err)
	}

	out := ScanOutcome{RanOK: map[domain.CheckerID]bool{"observation:CWE-A": true, "observation:CWE-B": true, "probe:subzy": true}}
	if !reflect.DeepEqual(base.Account(out), added.Account(out)) {
		t.Fatal("adding OWASP mapping changed coverage — P6 violated")
	}
}

// P4 (randomized): any outcome yields a valid partition over spec.Items.
func TestAccount_Property_ValidPartition(t *testing.T) {
	acc, _ := New(miniOwnership(), miniTable(t), miniSpec())
	checkers := []domain.CheckerID{"observation:CWE-A", "observation:CWE-B", "probe:subzy"}
	rng := rand.New(rand.NewSource(3))
	for range 300 {
		out := ScanOutcome{RanOK: map[domain.CheckerID]bool{}, Failed: map[domain.CheckerID]bool{}}
		for _, c := range checkers {
			switch rng.Intn(3) {
			case 0:
				out.RanOK[c] = true
			case 1:
				out.Failed[c] = true
			}
		}
		if err := acc.Account(out).Validate(acc.Spec()); err != nil {
			t.Fatalf("invalid partition for outcome %+v: %v", out, err)
		}
	}
}
