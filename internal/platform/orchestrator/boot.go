package orchestrator

import (
	"fmt"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/findings/compliance"
	fconfig "github.com/hongsam14/chiller/internal/findings/config"
	"github.com/hongsam14/chiller/internal/findings/coverage"
	"github.com/hongsam14/chiller/internal/scan/checking/registry"
)

// BootGate is the startup completeness check (PR-6, FR-N5c): it derives checker
// CWE-ownership from the registry, then runs coverage.New — which refuses to
// return an Accountant unless every spec item is reachable via weakness ∪
// attacker ∪ delegation (the 26-item partition). A failure here MUST abort
// startup (the composition root exits non-zero): a scanner that cannot account
// for all items must never accept a scan.
//
// It also returns the compliance Mapper used by the fold and the report builder,
// so the caller wires one loaded MappingTable everywhere.
func BootGate(checkers []domain.Checker, mt *fconfig.MappingTable, spec domain.CoverageSpec) (*coverage.Accountant, compliance.Mapper, error) {
	reg := registry.New()
	for _, c := range checkers {
		if err := reg.Register(c); err != nil {
			return nil, nil, fmt.Errorf("boot: register %s: %w", c.ID(), err)
		}
	}
	own := coverage.CWEOwnership(reg.OwnershipForFindings())
	acc, err := coverage.New(own, mt, spec)
	if err != nil {
		return nil, nil, fmt.Errorf("boot completeness failed (refusing to start): %w", err)
	}
	return acc, compliance.New(mt), nil
}
