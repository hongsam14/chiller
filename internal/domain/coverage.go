package domain

import "fmt"

// ItemStatus is the per-PS-ID coverage classification (US-020). The four
// statuses partition all 26 PS-IDs on every scan (R5).
type ItemStatus int

const (
	StatusChecked       ItemStatus = iota // chiller ran and judged the item (regardless of whether an issue was found)
	StatusNotApplicable                   // item does not apply to this target
	StatusDelegated                       // covered by another stage (carries Target, R6)
	StatusToolFailed                      // the owning checker errored before judging (US-027)
)

func (s ItemStatus) valid() bool { return s >= StatusChecked && s <= StatusToolFailed }

// ItemRecord is the coverage record for one PS-ID.
type ItemRecord struct {
	Status ItemStatus
	// Target is set iff Status == StatusDelegated (R6); empty otherwise.
	Target DelegateTarget
}

// Coverage is the accounting axis: the compliance items mapped to their record.
type Coverage map[PSID]ItemRecord

// CoverageSpec is the accounting-framework definition supplied from config — the
// full expected item set plus its fixed delegations. The kernel stays
// framework-agnostic: it provides the generic accounting machinery, while the
// customer-changeable item set (e.g. KISA's PS-001..026) and delegation map live
// in config, not here. Loading a spec is the only place "which items exist" is
// decided.
type CoverageSpec struct {
	Items       []PSID                  // the full expected item set for this scan's framework
	Delegations map[PSID]DelegateTarget // fixed delegations (R6): item → the stage that covers it
}

// Validate enforces rule R5 (totality over spec.Items + disjoint 4-state
// partition) and R6 (delegated ⇒ the spec's target). Generic — no hardcoded item
// list. Disjointness is structural (a map gives each item one status). Returns
// nil when the coverage is well-formed against the spec.
func (c Coverage) Validate(spec CoverageSpec) error {
	if len(c) != len(spec.Items) {
		return fmt.Errorf("coverage has %d items, want %d", len(c), len(spec.Items))
	}
	for _, id := range spec.Items {
		rec, ok := c[id]
		if !ok {
			return fmt.Errorf("coverage missing %s", id)
		}
		if !rec.Status.valid() {
			return fmt.Errorf("coverage %s has invalid status %d", id, rec.Status)
		}
		if rec.Status == StatusDelegated {
			want, ok := spec.Delegations[id]
			if !ok {
				return fmt.Errorf("coverage %s is delegated but is not a delegable item (R6)", id)
			}
			if rec.Target != want {
				return fmt.Errorf("coverage %s delegated to %q, want %q (R6)", id, rec.Target, want)
			}
		} else if rec.Target != "" {
			return fmt.Errorf("coverage %s is not delegated but carries target %q", id, rec.Target)
		}
	}
	return nil
}
