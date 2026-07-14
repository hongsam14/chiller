package compliance

import (
	"strings"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/findings/config"
)

const mp = `
version: "t"
map:
  CWE-615: { KISA: [PS-009] }
  CWE-540: { KISA: [PS-009] }
  no-canonical-cwe: { KISA: [] }
`

func newMapper(t *testing.T) Mapper {
	t.Helper()
	mt, err := config.LoadMapping(strings.NewReader(mp))
	if err != nil {
		t.Fatal(err)
	}
	return New(mt)
}

func TestProject_Dedupes(t *testing.T) {
	m := newMapper(t)
	got := m.Project([]domain.CWE{"CWE-615", "CWE-540"}, domain.FrameworkKISA)
	if len(got) != 1 || got[0] != "PS-009" {
		t.Errorf("Project=%v, want [PS-009]", got)
	}
}

// P7: round-trip — every projected item traces back to a CWE in the finding.
func TestProject_Property_RoundTrip(t *testing.T) {
	m := newMapper(t)
	cwes := []domain.CWE{"CWE-615"}
	items := m.Project(cwes, domain.FrameworkKISA)
	for _, item := range items {
		// PS-009 must be reachable from at least one of the finding's CWEs.
		found := false
		for _, c := range cwes {
			for _, it := range m.Project([]domain.CWE{c}, domain.FrameworkKISA) {
				if it == item {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("projected item %q not traceable to any finding CWE (P7)", item)
		}
	}
}

// P5: no-canonical projects to explicit empty, not a panic/missing.
func TestProject_NoCanonicalExplicitEmpty(t *testing.T) {
	m := newMapper(t)
	if got := m.Project([]domain.CWE{domain.NoCanonicalCWE}, domain.FrameworkKISA); len(got) != 0 {
		t.Errorf("no-canonical Project=%v, want []", got)
	}
	// unknown framework ⇒ empty (R10)
	if got := m.Project([]domain.CWE{"CWE-615"}, domain.FrameworkCAPEC); len(got) != 0 {
		t.Errorf("absent framework Project=%v, want []", got)
	}
}
