// Package report assembles the two-axis compliance report (US-019, FR-N6):
// findings on the CWE axis + coverage on the PS-ID axis + the mapping revision +
// framework labels (a projection, never the source of coverage — P6).
package report

import (
	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/findings/compliance"
)

// Report is the merged output served by the platform ScanAPI.
type Report struct {
	ScanID          domain.ScanID
	Findings        []domain.Finding // CWE axis (issues)
	Coverage        domain.Coverage  // PS-ID axis (accounting)
	MappingRevision string
	// FrameworkLabels projects findings onto a framework for the report — labels
	// only. framework → item → indices into Findings.
	FrameworkLabels map[domain.Framework]map[string][]int
}

// Builder is the ReportBuilder seam (NFR-01).
type Builder interface {
	Build(scanID domain.ScanID, findings []domain.Finding, cov domain.Coverage, frameworks []domain.Framework) Report
}

type builder struct{ mapper compliance.Mapper }

// New builds a Builder.
func New(mapper compliance.Mapper) Builder { return &builder{mapper: mapper} }

func (b *builder) Build(scanID domain.ScanID, findings []domain.Finding, cov domain.Coverage, frameworks []domain.Framework) Report {
	labels := make(map[domain.Framework]map[string][]int, len(frameworks))
	for _, fw := range frameworks {
		byItem := map[string][]int{}
		for i, f := range findings {
			for _, item := range b.mapper.Project(f.CWEs, fw) {
				byItem[item] = append(byItem[item], i)
			}
		}
		labels[fw] = byItem
	}
	return Report{
		ScanID:          scanID,
		Findings:        findings,
		Coverage:        cov,
		MappingRevision: b.mapper.Revision(),
		FrameworkLabels: labels,
	}
}
