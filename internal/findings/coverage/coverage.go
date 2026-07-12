// Package coverage accounts the PS-ID axis (US-020/021, P4/P6). Coverage is
// derived from checker OWNERSHIP (weakness axis = owned CWEs projected to KISA;
// attacker axis = the AttackTechnique owners), never from projecting finding
// CWEs — so adding a framework mapping leaves coverage bit-identical (P6).
package coverage

import (
	"fmt"
	"slices"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/findings/config"
)

// CWEOwnership declares which CWEs each checker emits (weakness axis, FQ1).
type CWEOwnership map[domain.CheckerID][]domain.CWE

// ScanOutcome is the per-scan input: which owning checkers ran ok / failed, and
// any structural not-applicable signals (FQ5; NotApplicable stays empty until
// the scan unit adds domain.WorkItemOutcome.NotApplicable).
type ScanOutcome struct {
	RanOK         map[domain.CheckerID]bool
	Failed        map[domain.CheckerID]bool
	NotApplicable map[domain.CheckerID]map[domain.PSID]bool
}

// Accountant produces domain.Coverage against a fixed CoverageSpec.
type Accountant struct {
	spec   domain.CoverageSpec
	owners map[domain.PSID][]domain.CheckerID // reverse of the responsibility sets
}

// New builds the responsibility sets and runs the boot completeness check
// (FR-N5c): every spec item must be reachable via weakness ∪ attacker ∪
// delegation, partitioned. Returns an error (fail startup) otherwise.
func New(ownership CWEOwnership, mt *config.MappingTable, spec domain.CoverageSpec) (*Accountant, error) {
	owners := map[domain.PSID]map[domain.CheckerID]bool{}
	add := func(id domain.PSID, c domain.CheckerID) {
		if owners[id] == nil {
			owners[id] = map[domain.CheckerID]bool{}
		}
		owners[id][c] = true
	}

	// Weakness axis: owned CWEs → KISA items.
	for checker, cwes := range ownership {
		for _, c := range cwes {
			if c != domain.NoCanonicalCWE && !mt.KnownCWE(c) {
				return nil, fmt.Errorf("coverage: checker %q owns unmapped CWE %q (P5 totality)", checker, c)
			}
		}
		for _, item := range mt.Project(cwes, domain.FrameworkKISA) {
			add(domain.PSID(item), checker)
		}
	}
	// Attacker axis: no-canonical owners.
	for id, owner := range mt.AttackOwners() {
		add(id, owner)
	}

	// Boot completeness: reachable ∪ delegated == spec.Items, and disjoint.
	itemSet := map[domain.PSID]bool{}
	for _, id := range spec.Items {
		itemSet[id] = true
	}
	for id := range owners {
		if !itemSet[id] {
			return nil, fmt.Errorf("coverage: %s is owned but not in the spec item set", id)
		}
		if _, delegated := spec.Delegations[id]; delegated {
			return nil, fmt.Errorf("coverage: %s is both owned and delegated (not disjoint, R5)", id)
		}
	}
	for _, id := range spec.Items {
		_, owned := owners[id]
		_, delegated := spec.Delegations[id]
		if !owned && !delegated {
			return nil, fmt.Errorf("coverage: %s is neither owned nor delegated (gap, R5 totality)", id)
		}
	}

	// Freeze reverse map (sorted for determinism).
	frozen := make(map[domain.PSID][]domain.CheckerID, len(owners))
	for id, set := range owners {
		list := make([]domain.CheckerID, 0, len(set))
		for c := range set {
			list = append(list, c)
		}
		slices.Sort(list)
		frozen[id] = list
	}
	return &Accountant{spec: spec, owners: frozen}, nil
}

// Account computes the coverage for one scan (FR-N5b). The result satisfies
// domain.Coverage.Validate(spec) by construction.
func (a *Accountant) Account(o ScanOutcome) domain.Coverage {
	cov := make(domain.Coverage, len(a.spec.Items))
	for _, id := range a.spec.Items {
		if t, ok := a.spec.Delegations[id]; ok {
			cov[id] = domain.ItemRecord{Status: domain.StatusDelegated, Target: t}
			continue
		}
		cov[id] = domain.ItemRecord{Status: a.statusFor(id, o)}
	}
	return cov
}

func (a *Accountant) statusFor(id domain.PSID, o ScanOutcome) domain.ItemStatus {
	na, ranOK := false, false
	for _, c := range a.owners[id] {
		if o.NotApplicable[c][id] {
			na = true
		}
		if o.RanOK[c] {
			ranOK = true
		}
	}
	switch {
	case na:
		return domain.StatusNotApplicable
	case ranOK:
		return domain.StatusChecked
	default:
		// owners exist (boot check) but none ran ok → all failed / not run.
		return domain.StatusToolFailed
	}
}

// Spec returns the coverage spec (for Validate at the report stage).
func (a *Accountant) Spec() domain.CoverageSpec { return a.spec }
