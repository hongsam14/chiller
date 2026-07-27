package config

import (
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

// The FSEC reference framework (과기정통부 고시 2025-0832 웹 평가) must stay internally
// consistent AND honest about what this passive engine can judge:
//   - the catalog covers every item;
//   - every item is either delegated OR maps to a CWE some passive checker owns;
//   - a DELEGATED item maps to NO owned CWE (else we delegated something we can
//     actually detect — a coverage understatement).
//
// "Owned CWEs" are taken from the live KISA mapping, whose CWE set is exactly the
// checker-owned weakness axis, so this test tracks reality without hardcoding.
func TestFSEC_HonestPartition(t *testing.T) {
	const (
		covPath  = "../../../configs/coverage/fsec.yaml"
		mapPath  = "../../../configs/mappings/fsec.yaml"
		kisaPath = "../../../configs/mappings/kisa.yaml"
	)

	spec, err := LoadCoverageSpecFile(covPath)
	if err != nil {
		t.Fatalf("fsec coverage parse: %v", err)
	}
	cat, err := LoadCatalogFile(covPath)
	if err != nil {
		t.Fatalf("fsec catalog parse: %v", err)
	}
	fsecMT, err := LoadMappingFile(mapPath)
	if err != nil {
		t.Fatalf("fsec mapping parse: %v", err)
	}
	kisaMT, err := LoadMappingFile(kisaPath)
	if err != nil {
		t.Fatalf("kisa mapping parse: %v", err)
	}

	owned := map[domain.CWE]bool{}
	for _, cwes := range kisaMT.ItemCWEs(domain.FrameworkKISA) {
		for _, c := range cwes {
			if c != domain.NoCanonicalCWE {
				owned[c] = true
			}
		}
	}

	const fw = domain.Framework("FSEC")
	itemCWEs := fsecMT.ItemCWEs(fw)

	for _, id := range spec.Items {
		if m, ok := cat[id]; !ok || m.Title == "" || m.Description == "" || m.Category == "" {
			t.Errorf("%s: catalog entry missing/incomplete (%+v)", id, m)
		}
		cwes := itemCWEs[id]
		if len(cwes) == 0 {
			t.Errorf("%s: no CWE mapped in fsec mapping", id)
			continue
		}
		hasOwned := false
		for _, c := range cwes {
			if owned[c] {
				hasOwned = true
			}
		}
		_, delegated := spec.Delegations[id]
		switch {
		case delegated && hasOwned:
			t.Errorf("%s is delegated but maps to an owned CWE %v — passive engine can detect it, understated", id, cwes)
		case !delegated && !hasOwned:
			t.Errorf("%s is not delegated but no passive checker owns any of its CWEs %v — boot gate would refuse (false coverage)", id, cwes)
		}
	}

	// Guard the documented split explicitly so a future edit can't quietly flip it.
	wantDelegated := map[domain.PSID]bool{"WEB-SER-041": true, "WEB-SER-046": true, "WEB-SER-048": true, "WEB-SER-049": true, "WEB-SER-052": true}
	for id := range wantDelegated {
		if _, ok := spec.Delegations[id]; !ok {
			t.Errorf("%s should be delegated (active/dynamic check)", id)
		}
	}
	if len(spec.Delegations) != len(wantDelegated) {
		t.Errorf("delegations = %d, want %d (only 040/051 are passively covered)", len(spec.Delegations), len(wantDelegated))
	}
}
