// Package normalize turns a checker's structured emission into a validated,
// CWE-keyed domain.Finding (US-014, P1). Tool-specific parsing lives in the scan
// adapters; this unit's contract is the RawFinding → Finding normalization.
package normalize

import (
	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/findings/severity"
)

// RawFinding is what a scan adapter produces after parsing its tool's output —
// framework-agnostic, pre-normalization.
type RawFinding struct {
	CWEs        []domain.CWE
	TargetURL   string
	SourceTool  string
	RawSeverity string
	Evidence    []string
}

// Normalizer is the seam (NFR-01).
type Normalizer interface {
	// Normalize is total (P1): it never panics; an emission that cannot form a
	// valid Finding returns an error (the caller drops it and counts).
	Normalize(RawFinding) (domain.Finding, error)
	// NormalizeAll normalizes a batch, dropping invalid ones and returning the
	// dropped count (diagnostic, never a crash).
	NormalizeAll([]RawFinding) (out []domain.Finding, dropped int)
}

type normalizer struct {
	sev         severity.Mapper
	maxSnippets int
}

// New builds a Normalizer.
func New(sev severity.Mapper) Normalizer {
	return &normalizer{sev: sev, maxSnippets: domain.MaxEvidenceSnippets}
}

func (n *normalizer) Normalize(raw RawFinding) (domain.Finding, error) {
	f := domain.Finding{
		CWEs:       domain.NormalizeCWEs(raw.CWEs),
		TargetURL:  raw.TargetURL,
		SourceTool: raw.SourceTool,
		Severity:   n.sev.Map(raw.RawSeverity, raw.SourceTool),
		Evidence:   domain.Evidence{Snippets: bound(raw.Evidence, n.maxSnippets)},
	}
	if err := f.Validate(); err != nil {
		return domain.Finding{}, err
	}
	return f, nil
}

func (n *normalizer) NormalizeAll(raws []RawFinding) ([]domain.Finding, int) {
	out := make([]domain.Finding, 0, len(raws))
	dropped := 0
	for _, r := range raws {
		f, err := n.Normalize(r)
		if err != nil {
			dropped++
			continue
		}
		out = append(out, f)
	}
	return out, dropped
}

// bound truncates evidence snippets to the limit (FR-12/R9). Fresh slice.
func bound(snips []string, max int) []string {
	if len(snips) <= max {
		return append([]string(nil), snips...)
	}
	return append([]string(nil), snips[:max]...)
}
