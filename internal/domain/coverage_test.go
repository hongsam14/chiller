package domain

import (
	"fmt"
	"testing"
)

// testSpec mirrors what config would supply for KISA (PS-001..026 + delegations).
// It lives in the test, not the kernel — domain no longer hardcodes the item set.
func testSpec() CoverageSpec {
	items := make([]PSID, 0, 26)
	for i := 1; i <= 26; i++ {
		items = append(items, PSID(fmt.Sprintf("PS-%03d", i)))
	}
	return CoverageSpec{
		Items: items,
		Delegations: map[PSID]DelegateTarget{
			"PS-014": DelegateASM,
			"PS-005": DelegateNuclei,
			"PS-006": DelegateNuclei,
			"PS-026": DelegateNuclei,
		},
	}
}

// fullCoverage builds a well-formed coverage over the spec, every item Checked.
func fullCoverage() Coverage {
	c := make(Coverage, len(testSpec().Items))
	for _, id := range testSpec().Items {
		c[id] = ItemRecord{Status: StatusChecked}
	}
	return c
}

func TestCoverageValidate_OK(t *testing.T) {
	c := fullCoverage()
	// Realistic delegations (R6), consistent with the spec.
	c["PS-014"] = ItemRecord{Status: StatusDelegated, Target: DelegateASM}
	c["PS-005"] = ItemRecord{Status: StatusDelegated, Target: DelegateNuclei}
	c["PS-006"] = ItemRecord{Status: StatusDelegated, Target: DelegateNuclei}
	c["PS-026"] = ItemRecord{Status: StatusDelegated, Target: DelegateNuclei}
	c["PS-004"] = ItemRecord{Status: StatusToolFailed}
	c["PS-020"] = ItemRecord{Status: StatusNotApplicable}
	if err := c.Validate(testSpec()); err != nil {
		t.Fatalf("expected valid coverage, got: %v", err)
	}
}

func TestCoverageValidate_MissingItem(t *testing.T) {
	c := fullCoverage()
	delete(c, "PS-013")
	if err := c.Validate(testSpec()); err == nil {
		t.Error("expected error for missing PS-013")
	}
}

func TestCoverageValidate_WrongCount(t *testing.T) {
	c := fullCoverage()
	c["PS-999"] = ItemRecord{Status: StatusChecked} // one more than the spec
	if err := c.Validate(testSpec()); err == nil {
		t.Error("expected error for extra item")
	}
}

func TestCoverageValidate_DelegatedWithoutTarget(t *testing.T) {
	c := fullCoverage()
	c["PS-014"] = ItemRecord{Status: StatusDelegated} // no target
	if err := c.Validate(testSpec()); err == nil {
		t.Error("expected error for delegated item without target (R6)")
	}
}

func TestCoverageValidate_TargetWithoutDelegation(t *testing.T) {
	c := fullCoverage()
	c["PS-001"] = ItemRecord{Status: StatusChecked, Target: DelegateASM} // target but not delegated
	if err := c.Validate(testSpec()); err == nil {
		t.Error("expected error for non-delegated item carrying a target (R6)")
	}
}

func TestCoverageValidate_NonDelegableItemDelegated(t *testing.T) {
	// R6: only items in spec.Delegations may be delegated. PS-001 may not.
	c := fullCoverage()
	c["PS-001"] = ItemRecord{Status: StatusDelegated, Target: DelegateNuclei}
	if err := c.Validate(testSpec()); err == nil {
		t.Error("expected error: PS-001 is not a delegable item (R6)")
	}
}

func TestCoverageValidate_WrongDelegateTarget(t *testing.T) {
	// R6: PS-014 must delegate to ASM (per spec), not Nuclei.
	c := fullCoverage()
	c["PS-014"] = ItemRecord{Status: StatusDelegated, Target: DelegateNuclei}
	if err := c.Validate(testSpec()); err == nil {
		t.Error("expected error: PS-014 must delegate to ASM, not Nuclei (R6)")
	}
	// PS-005 must delegate to Nuclei, not ASM.
	c = fullCoverage()
	c["PS-005"] = ItemRecord{Status: StatusDelegated, Target: DelegateASM}
	if err := c.Validate(testSpec()); err == nil {
		t.Error("expected error: PS-005 must delegate to Nuclei, not ASM (R6)")
	}
}

func TestCoverageValidate_InvalidStatus(t *testing.T) {
	c := fullCoverage()
	c["PS-002"] = ItemRecord{Status: ItemStatus(99)}
	if err := c.Validate(testSpec()); err == nil {
		t.Error("expected error for invalid status")
	}
}

// A different (smaller) spec validates against its own item set — proving the
// kernel is not tied to KISA's 26.
func TestCoverageValidate_ArbitrarySpec(t *testing.T) {
	spec := CoverageSpec{Items: []PSID{"X-1", "X-2"}, Delegations: map[PSID]DelegateTarget{"X-2": DelegateASM}}
	c := Coverage{"X-1": {Status: StatusChecked}, "X-2": {Status: StatusDelegated, Target: DelegateASM}}
	if err := c.Validate(spec); err != nil {
		t.Fatalf("expected valid coverage for custom spec, got: %v", err)
	}
}
