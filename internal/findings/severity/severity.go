// Package severity maps a tool's raw severity to a domain.Severity (US-016, P3).
package severity

import (
	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/findings/config"
)

// Mapper is the SeverityMapper seam (NFR-01).
type Mapper interface {
	// Map is total (P3, R4): unmapped (raw, tool) ⇒ the config default (Info).
	Map(rawSeverity, sourceTool string) domain.Severity
}

type mapper struct{ cfg *config.SeverityConfig }

// New builds a Mapper over a loaded severity config.
func New(cfg *config.SeverityConfig) Mapper { return &mapper{cfg: cfg} }

func (m *mapper) Map(raw, tool string) domain.Severity { return m.cfg.Map(raw, tool) }
