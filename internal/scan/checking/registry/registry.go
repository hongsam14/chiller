// Package registry holds the checker plugin registry (NFR-02). It also derives
// the CWEOwnership map that findings' CoverageAccountant consumes at boot.
package registry

import "github.com/hongsam14/chiller/internal/domain"

// owned is implemented by checkers that can declare their CWEs (observation
// signature-checkers and probe adapters). The registry uses it to build
// ownership without the orchestrator knowing concrete checker types.
type owned interface{ OwnedCWEs() []domain.CWE }

// Registry is the checker plugin seam.
type Registry interface {
	Register(domain.Checker) error
	List() []domain.Checker
	// OwnershipForFindings maps each checker id to the CWEs it owns
	// (→ findings.coverage.CWEOwnership). Checkers that don't declare ownership
	// are omitted.
	OwnershipForFindings() map[domain.CheckerID][]domain.CWE
}

type registry struct {
	checkers []domain.Checker
	seen     map[domain.CheckerID]bool
}

// New builds an empty registry.
func New() Registry { return &registry{seen: map[domain.CheckerID]bool{}} }

func (r *registry) Register(c domain.Checker) error {
	if r.seen[c.ID()] {
		return nil // idempotent
	}
	r.seen[c.ID()] = true
	r.checkers = append(r.checkers, c)
	return nil
}

func (r *registry) List() []domain.Checker {
	out := make([]domain.Checker, len(r.checkers))
	copy(out, r.checkers)
	return out
}

func (r *registry) OwnershipForFindings() map[domain.CheckerID][]domain.CWE {
	out := map[domain.CheckerID][]domain.CWE{}
	for _, c := range r.checkers {
		if o, ok := c.(owned); ok {
			out[c.ID()] = o.OwnedCWEs()
		}
	}
	return out
}
