package config

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/enki-offen/chiller/internal/domain"
	"gopkg.in/yaml.v3"
)

// SeverityConfig maps a tool's raw severity strings to domain.Severity, per
// tool, with a total fallback (FQ4=B, R4). It is the SeverityMapper's data.
type SeverityConfig struct {
	perTool map[string]map[string]domain.Severity
	def     domain.Severity
}

type severityFile struct {
	Default string                       `yaml:"default"`
	Tools   map[string]map[string]string `yaml:"tools"`
}

// parseSeverity is the inverse of domain.Severity.String (case-insensitive).
func parseSeverity(s string) (domain.Severity, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "info":
		return domain.SevInfo, true
	case "low":
		return domain.SevLow, true
	case "medium":
		return domain.SevMedium, true
	case "high":
		return domain.SevHigh, true
	case "critical":
		return domain.SevCritical, true
	default:
		return domain.SevInfo, false
	}
}

// LoadSeverity parses a severity config file.
func LoadSeverity(r io.Reader) (*SeverityConfig, error) {
	var f severityFile
	if err := yaml.NewDecoder(r).Decode(&f); err != nil {
		return nil, fmt.Errorf("severity: %w", err)
	}
	def, ok := parseSeverity(f.Default)
	if !ok {
		def = domain.SevInfo // R4: unmappable default is Info
	}
	c := &SeverityConfig{perTool: make(map[string]map[string]domain.Severity, len(f.Tools)), def: def}
	for tool, table := range f.Tools {
		m := make(map[string]domain.Severity, len(table))
		for raw, name := range table {
			sev, ok := parseSeverity(name)
			if !ok {
				return nil, fmt.Errorf("severity: tool %q raw %q maps to unknown severity %q", tool, raw, name)
			}
			m[raw] = sev
		}
		c.perTool[tool] = m
	}
	return c, nil
}

// LoadSeverityFile is a path convenience over LoadSeverity.
func LoadSeverityFile(path string) (*SeverityConfig, error) {
	// #nosec G304 -- path is an operator-supplied config location (a CLI flag),
	// inside the trust boundary. It is not user input, and constraining it to a
	// fixed directory would break the legitimate case of configs living elsewhere
	// while buying no real defence.
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }() // read-only: a failed close cannot lose data
	return LoadSeverity(fh)
}

// Map is a total function (P3, R4): unmapped (raw, tool) ⇒ the default (Info).
func (c *SeverityConfig) Map(raw, tool string) domain.Severity {
	if m, ok := c.perTool[tool]; ok {
		if s, ok := m[raw]; ok {
			return s
		}
	}
	return c.def
}
