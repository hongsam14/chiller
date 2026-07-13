// Package compliance projects CWEs onto framework items (US-017/018, P5/P7).
// Report-labeling only — never coverage (P6).
package compliance

import (
	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/findings/config"
)

// Mapper is the ComplianceMapper seam (NFR-01).
type Mapper interface {
	// Project returns the framework items for the finding's CWEs. Totality (P5):
	// a known CWE yields ≥1 item or an explicit empty — never a missing key.
	Project(cwes []domain.CWE, fw domain.Framework) []string
	Revision() string
}

type mapper struct{ t *config.MappingTable }

// New builds a Mapper over a loaded mapping table.
func New(t *config.MappingTable) Mapper { return &mapper{t: t} }

func (m *mapper) Project(cwes []domain.CWE, fw domain.Framework) []string {
	return m.t.Project(cwes, fw)
}

func (m *mapper) Revision() string { return m.t.Revision() }
