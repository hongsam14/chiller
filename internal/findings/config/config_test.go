package config

import (
	"strings"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

const mappingYAML = `
version: "test-1"
map:
  CWE-615: { KISA: [PS-009] }
  CWE-540: { KISA: [PS-009] }
  CWE-614: { KISA: [PS-015] }
  no-canonical-cwe: { KISA: [] }
attackTechnique:
  PS-025:
    owner: "probe:subzy"
    labels:
      ATTACK: [T1584.001]
`

func TestLoadMapping_ProjectAndAttack(t *testing.T) {
	mt, err := LoadMapping(strings.NewReader(mappingYAML))
	if err != nil {
		t.Fatal(err)
	}
	if mt.Revision() != "test-1" {
		t.Errorf("revision=%q", mt.Revision())
	}
	// Two CWEs mapping to the same item dedupe.
	got := mt.Project([]domain.CWE{"CWE-615", "CWE-540"}, domain.FrameworkKISA)
	if len(got) != 1 || got[0] != "PS-009" {
		t.Errorf("Project=%v, want [PS-009]", got)
	}
	// no-canonical projects to empty (explicit, not missing).
	if got := mt.Project([]domain.CWE{domain.NoCanonicalCWE}, domain.FrameworkKISA); len(got) != 0 {
		t.Errorf("no-canonical Project=%v, want []", got)
	}
	// Attacker axis owner.
	if owners := mt.AttackOwners(); owners["PS-025"] != "probe:subzy" {
		t.Errorf("AttackOwners[PS-025]=%q", owners["PS-025"])
	}
	if !mt.KnownCWE("CWE-614") || mt.KnownCWE("CWE-9999") {
		t.Error("KnownCWE wrong")
	}
}

func TestLoadSeverity(t *testing.T) {
	c, err := LoadSeverity(strings.NewReader("default: Info\ntools:\n  testssl: { HIGH: High }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Map("HIGH", "testssl") != domain.SevHigh {
		t.Error("testssl HIGH should map to SevHigh")
	}
	if c.Map("weird", "testssl") != domain.SevInfo {
		t.Error("unmapped raw should fall back to Info")
	}
	if c.Map("HIGH", "unknown-tool") != domain.SevInfo {
		t.Error("unknown tool should fall back to Info")
	}
}

func TestLoadCoverageSpec(t *testing.T) {
	spec, err := LoadCoverageSpec(strings.NewReader("items: [PS-001, PS-014]\ndelegations: { PS-014: ASM }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Items) != 2 || spec.Items[0] != "PS-001" {
		t.Errorf("items=%v", spec.Items)
	}
	if spec.Delegations["PS-014"] != domain.DelegateASM {
		t.Errorf("delegation=%v", spec.Delegations["PS-014"])
	}
}
