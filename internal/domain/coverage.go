package domain

import "fmt"

// ItemStatus is the per-PS-ID coverage classification (US-020). The four
// statuses partition all 26 PS-IDs on every scan (R5).
type ItemStatus int

const (
	// StatusUnset is the zero value and is NEVER valid. It exists so that the
	// zero value cannot be mistaken for a real status.
	//
	// This matters more than it looks. A BSON decoder does not invoke a custom
	// unmarshaler for a field that is absent from the document — it simply leaves
	// the Go zero value in place. If StatusChecked were the zero value (it used to
	// be), a coverage record whose status field went missing would silently read
	// as "chiller checked this item and judged it": a storage fault would turn
	// into a compliance FALSE NEGATIVE, which is the single worst way this system
	// can be wrong. With StatusUnset at zero, that same document fails
	// Coverage.Validate (R5) instead.
	StatusUnset         ItemStatus = iota
	StatusChecked                  // chiller ran and judged the item (regardless of whether an issue was found)
	StatusNotApplicable            // item does not apply to this target
	StatusDelegated                // covered by another stage (carries Target, R6)
	StatusToolFailed               // the owning checker errored before judging (US-027)
)

func (s ItemStatus) valid() bool { return s >= StatusChecked && s <= StatusToolFailed }

// itemStatusNames are the persisted and wire forms (G2) — index 0 (StatusUnset)
// deliberately has no name, so it can neither be written nor parsed.
var itemStatusNames = [...]string{"", "checked", "not_applicable", "delegated", "tool_failed"}

// String returns the canonical name, or "unknown" for the unset/out-of-range values.
func (s ItemStatus) String() string {
	if !s.valid() {
		return "unknown"
	}
	return itemStatusNames[s]
}

// ParseItemStatus is the inverse of String. An unknown or empty name is rejected,
// never defaulted (see StatusUnset).
func ParseItemStatus(name string) (ItemStatus, bool) {
	if name == "" {
		return StatusUnset, false
	}
	for i, n := range itemStatusNames {
		if n == name {
			return ItemStatus(i), true
		}
	}
	return StatusUnset, false
}

// ItemRecord is the coverage record for one PS-ID.
type ItemRecord struct {
	Status ItemStatus `bson:"status" json:"status"`
	// Target is set iff Status == StatusDelegated (R6); empty otherwise.
	Target DelegateTarget `bson:"target,omitempty" json:"target,omitempty"`
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
	// Framework names which projection the accountant uses to map owned CWEs → these
	// items (the mapping file must carry that framework's columns). Empty means
	// FrameworkKISA — the historical default, so pre-existing specs are unchanged.
	Framework Framework
}

// FrameworkOrDefault returns the spec's framework, defaulting to KISA when unset
// (so a coverage file that omits `framework:` keeps the legacy behaviour).
func (s CoverageSpec) FrameworkOrDefault() Framework {
	if s.Framework == "" {
		return FrameworkKISA
	}
	return s.Framework
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
