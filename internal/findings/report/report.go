// Package report assembles the two-axis compliance report (US-019, FR-N6):
// findings on the CWE axis + coverage on the PS-ID axis + the mapping revision +
// framework labels (a projection, never the source of coverage — P6).
package report

import (
	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/findings/compliance"
)

// ReportSchemaVersion marks the report's wire layout. It changed when
// FrameworkLabels stopped being positional (see below), so a consumer can tell
// the two apart instead of silently misreading one as the other.
const ReportSchemaVersion = 2

// Report is the merged output served by the platform ScanAPI.
//
// Findings are NOT stored with the rest of this struct: the durable TaskStore
// writes the report's skeleton and the FindingsStore owns the findings, and the
// two are joined on read (data-census O-1 — evidence lives in one place). That is
// also why FrameworkLabels references findings by FindingKey rather than by index
// into the Findings slice, which is what it used to do: an index is only meaningful
// if the findings come back in the order they were written, and pinning a report's
// correctness to a store's iteration order is a bug waiting for a backend swap.
type Report struct {
	ScanID          domain.ScanID
	Findings        []domain.Finding // CWE axis (issues)
	Coverage        domain.Coverage  // PS-ID axis (accounting)
	MappingRevision string
	// FrameworkLabels projects findings onto a framework for the report — labels
	// only. framework → item → the keys of the findings that carry it. Resolve a
	// key against Findings with domain.Finding.Key().
	FrameworkLabels map[domain.Framework]map[string][]domain.FindingKey
	// Frameworks is the accounting framework(s) this report was built for — the
	// Coverage is that framework's item partition. A consumer (e.g. the report view)
	// reads it to know which framework's labels/catalog to project, since the
	// framework is now a per-scan choice, not a process-global.
	Frameworks    []domain.Framework `bson:"frameworks,omitempty" json:"frameworks,omitempty"`
	SchemaVersion int
	// TruncatedURLs lists the scanned URLs whose body was not fully available as
	// decoded text (r17, FR-T4) — a compressed body we could not fully decode, an
	// unknown Content-Encoding, or a body past the size cap. A body-based "no finding"
	// on these URLs is only a PARTIAL result, so the report surfaces them rather than
	// letting a truncated page read as an authoritative clean scan. Set by the
	// orchestrator after Build, so the Builder interface stays unchanged.
	TruncatedURLs []string `bson:"truncatedUrls,omitempty" json:"truncatedUrls,omitempty"`
}

// Builder is the ReportBuilder seam (NFR-01).
type Builder interface {
	Build(scanID domain.ScanID, findings []domain.Finding, cov domain.Coverage, frameworks []domain.Framework) Report
}

type builder struct{ mapper compliance.Mapper }

// New builds a Builder.
func New(mapper compliance.Mapper) Builder { return &builder{mapper: mapper} }

func (b *builder) Build(scanID domain.ScanID, findings []domain.Finding, cov domain.Coverage, frameworks []domain.Framework) Report {
	labels := make(map[domain.Framework]map[string][]domain.FindingKey, len(frameworks))
	for _, fw := range frameworks {
		byItem := map[string][]domain.FindingKey{}
		for _, f := range findings {
			for _, item := range b.mapper.Project(f.CWEs, fw) {
				byItem[item] = append(byItem[item], f.Key())
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
		Frameworks:      frameworks,
		SchemaVersion:   ReportSchemaVersion,
	}
}
