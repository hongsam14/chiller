package api

import (
	"net/http"
	"sort"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/findings/report"
)

// The compliance report view is the CUSTOMER-facing projection of a scan report
// (distinct from the operator queue dashboard). It joins three things the raw
// report keeps separate: the per-item accounting status (Coverage), the issues
// found (Findings, via FrameworkLabels), and the human catalog (title/CWE/
// category). Its job is to answer the two questions operators actually asked for:
// "did we find a vulnerability on this item, or not?" and "for the items we did
// not judge here, why — and who covers them?".

// itemResult is the customer-facing outcome, derived from the raw ItemStatus plus
// whether any finding landed on the item. It splits the ambiguous "checked" into
// vulnerable vs clean, and keeps delegated/not-applicable distinct so the report
// can present them honestly as routing, not failure.
const (
	resultVulnerable    = "vulnerable"     // judged here, ≥1 finding
	resultClean         = "clean"          // judged here, no finding
	resultDelegated     = "delegated"      // covered by another engine (Nuclei/ASM), by design
	resultNotApplicable = "not_applicable" // the precondition is absent on this target
	resultToolFailed    = "tool_failed"    // the owning tool could not run → no verdict
)

// findingDTO is one concrete finding under an item — the evidence a customer
// drills into to confirm a vulnerability (which URL, which tool, what was seen).
type findingDTO struct {
	TargetURL  string   `json:"targetUrl"`
	SourceTool string   `json:"sourceTool"`
	Severity   string   `json:"severity"`
	CWEs       []string `json:"cwes"`
	Evidence   []string `json:"evidence,omitempty"`
}

type reportItemDTO struct {
	PSID        string       `json:"psid"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Category    string       `json:"category"`
	CWEs        []string     `json:"cwes"`
	Status      string       `json:"status"` // raw accounting status
	Result      string       `json:"result"` // derived customer-facing result
	DelegateTo  string       `json:"delegateTo,omitempty"`
	Findings    int          `json:"findings"`
	TopSeverity string       `json:"topSeverity,omitempty"`
	ActiveHint  bool         `json:"activeHint,omitempty"`
	Details     []findingDTO `json:"details,omitempty"` // the item's findings (for drill-down)
}

type reportSummaryDTO struct {
	Total         int            `json:"total"`
	Vulnerable    int            `json:"vulnerable"`
	Clean         int            `json:"clean"`
	Delegated     int            `json:"delegated"`
	NotApplicable int            `json:"notApplicable"`
	ToolFailed    int            `json:"toolFailed"`
	Findings      int            `json:"findings"`
	BySeverity    map[string]int `json:"bySeverity"`
}

type complianceReportDTO struct {
	ScanID          string           `json:"scanId"`
	Ready           bool             `json:"ready"`
	Framework       string           `json:"framework"`
	MappingRevision string           `json:"mappingRevision,omitempty"`
	Summary         reportSummaryDTO `json:"summary"`
	Items           []reportItemDTO  `json:"items"`
	// TruncatedURLs (r17, FR-T4): URLs whose body was only partially available (a
	// body we could not fully decode, an unknown Content-Encoding, or a body past the
	// size cap). A body-based "clean" on these is only a PARTIAL result — the view
	// warns on them so a truncated page is not read as an authoritative clean scan.
	TruncatedURLs []string `json:"truncatedUrls,omitempty"`
}

// getComplianceReport serves the enriched, customer-facing report DTO. 202 while
// the scan is still running (mirrors /scans/{id}/report), 200 when complete.
func (h *Handler) getComplianceReport(w http.ResponseWriter, r *http.Request) {
	setJSONSecurityHeaders(w)
	id := domain.ScanID(r.PathValue("id"))
	rep, ready, err := h.orch.GetReport(r.Context(), id)
	if err != nil {
		h.writeOrchErr(w, err)
		return
	}
	if !ready {
		writeJSON(w, http.StatusAccepted, complianceReportDTO{ScanID: string(id), Ready: false})
		return
	}
	writeJSON(w, http.StatusOK, h.buildComplianceReport(rep))
}

// buildComplianceReport is the pure join. It never invents data: an item with no
// catalog entry keeps its bare PS-ID as the title, and the derived result follows
// strictly from (status, findings-on-item).
func (h *Handler) buildComplianceReport(rep report.Report) complianceReportDTO {
	// Index findings by key so an item's FrameworkLabels keys resolve to severities.
	byKey := make(map[domain.FindingKey]domain.Finding, len(rep.Findings))
	for _, f := range rep.Findings {
		byKey[f.Key()] = f
	}
	// The framework is a per-scan choice: read it from the report, falling back to
	// the handler default for older reports that predate the field.
	fw := h.reportFramework()
	if len(rep.Frameworks) > 0 && rep.Frameworks[0] != "" {
		fw = rep.Frameworks[0]
	}
	labels := rep.FrameworkLabels[fw] // item → finding keys (may be nil)

	dto := complianceReportDTO{
		ScanID:          string(rep.ScanID),
		Ready:           true,
		Framework:       string(fw),
		MappingRevision: rep.MappingRevision,
		Summary:         reportSummaryDTO{BySeverity: map[string]int{}},
		TruncatedURLs:   rep.TruncatedURLs,
	}

	ids := make([]string, 0, len(rep.Coverage))
	for id := range rep.Coverage {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)

	for _, id := range ids {
		psid := domain.PSID(id)
		rec := rep.Coverage[psid]
		meta := h.catalog[psid]

		keys := labels[id]
		topSev, sevName := domain.SevUnset, ""
		var details []findingDTO
		for _, k := range keys {
			f, ok := byKey[k]
			if !ok {
				continue
			}
			if f.Severity > topSev {
				topSev = f.Severity
			}
			details = append(details, findingDTO{
				TargetURL:  f.TargetURL,
				SourceTool: f.SourceTool,
				Severity:   f.Severity.String(),
				CWEs:       cweStrings(f.CWEs),
				Evidence:   f.Evidence.Snippets,
			})
		}
		if topSev != domain.SevUnset {
			sevName = topSev.String()
		}

		item := reportItemDTO{
			PSID:        id,
			Title:       fallback(meta.Title, id),
			Description: meta.Description,
			Category:    meta.Category,
			CWEs:        cweStrings(h.itemCWEs[psid]),
			Status:      rec.Status.String(),
			DelegateTo:  string(rec.Target),
			Findings:    len(keys),
			TopSeverity: sevName,
			ActiveHint:  meta.ActiveHint,
			Details:     details,
		}
		item.Result = deriveResult(rec.Status, len(keys))
		dto.Items = append(dto.Items, item)

		// Summary tallies.
		dto.Summary.Total++
		switch item.Result {
		case resultVulnerable:
			dto.Summary.Vulnerable++
		case resultClean:
			dto.Summary.Clean++
		case resultDelegated:
			dto.Summary.Delegated++
		case resultNotApplicable:
			dto.Summary.NotApplicable++
		case resultToolFailed:
			dto.Summary.ToolFailed++
		}
	}

	// Report-wide findings + severity histogram (each finding counted once).
	dto.Summary.Findings = len(rep.Findings)
	for _, f := range rep.Findings {
		dto.Summary.BySeverity[f.Severity.String()]++
	}
	return dto
}

// deriveResult splits the ambiguous "checked" into vulnerable vs clean and passes
// the other statuses through unchanged.
func deriveResult(status domain.ItemStatus, findings int) string {
	switch status {
	case domain.StatusChecked:
		if findings > 0 {
			return resultVulnerable
		}
		return resultClean
	case domain.StatusDelegated:
		return resultDelegated
	case domain.StatusNotApplicable:
		return resultNotApplicable
	default:
		return resultToolFailed
	}
}

func cweStrings(cwes []domain.CWE) []string {
	if len(cwes) == 0 {
		return nil
	}
	out := make([]string, len(cwes))
	for i, c := range cwes {
		out[i] = string(c)
	}
	sort.Strings(out)
	return out
}

func fallback(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}
