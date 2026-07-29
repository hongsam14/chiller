package api

import (
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
	fconfig "github.com/hongsam14/chiller/internal/findings/config"
	"github.com/hongsam14/chiller/internal/findings/report"
)

// buildComplianceReport must (1) split "checked" into vulnerable vs clean by
// whether a finding landed on the item, (2) keep delegated/not-applicable/
// tool-failed distinct, and (3) join catalog title + CWE badges without inventing
// data. This is the customer-facing honesty contract.
// r17 (FR-T4): the report DTO surfaces the truncated/partial URLs so the view can warn
// that a body-based "clean" on them is only a partial result.
func TestBuildComplianceReport_SurfacesTruncatedURLs(t *testing.T) {
	rep := report.Report{
		ScanID:        "s1",
		TruncatedURLs: []string{"https://h/big.js", "https://h/page"},
	}
	dto := (&Handler{}).buildComplianceReport(rep)
	if len(dto.TruncatedURLs) != 2 || dto.TruncatedURLs[0] != "https://h/big.js" {
		t.Errorf("truncated URLs not surfaced in DTO: %+v", dto.TruncatedURLs)
	}
}

func TestBuildComplianceReport_DerivesResultsAndJoins(t *testing.T) {
	vuln := domain.Finding{CWEs: []domain.CWE{"CWE-538"}, TargetURL: "https://h/.env", SourceTool: "observation:CWE-538", Severity: domain.SevCritical}

	rep := report.Report{
		ScanID:          "s1",
		MappingRevision: "kisa-test",
		Findings:        []domain.Finding{vuln},
		Coverage: domain.Coverage{
			"PS-011": {Status: domain.StatusChecked},                                  // vulnerable (has finding)
			"PS-002": {Status: domain.StatusChecked},                                  // clean (no finding)
			"PS-005": {Status: domain.StatusDelegated, Target: domain.DelegateNuclei}, // delegated → Nuclei
			"PS-023": {Status: domain.StatusNotApplicable},                            // not applicable (+activeHint)
			"PS-025": {Status: domain.StatusToolFailed},                               // tool failed
		},
		FrameworkLabels: map[domain.Framework]map[string][]domain.FindingKey{
			domain.FrameworkKISA: {"PS-011": {vuln.Key()}},
		},
	}

	h := &Handler{
		catalog: map[domain.PSID]fconfig.ItemMeta{
			"PS-011": {Title: ".env 노출", Category: "정보 노출", Description: "d"},
			"PS-023": {Title: "CAPTCHA 부재", ActiveHint: true},
		},
		itemCWEs: map[domain.PSID][]domain.CWE{"PS-011": {"CWE-538"}},
	}

	dto := h.buildComplianceReport(rep)

	byID := map[string]reportItemDTO{}
	for _, it := range dto.Items {
		byID[it.PSID] = it
	}
	if got := byID["PS-011"]; got.Result != resultVulnerable || got.TopSeverity != "critical" || got.Findings != 1 {
		t.Errorf("PS-011 want vulnerable/critical/1, got %s/%s/%d", got.Result, got.TopSeverity, got.Findings)
	}
	// Drill-down: the vulnerable item carries the concrete finding detail.
	if got := byID["PS-011"]; len(got.Details) != 1 ||
		got.Details[0].TargetURL != "https://h/.env" ||
		got.Details[0].SourceTool != "observation:CWE-538" ||
		got.Details[0].Severity != "critical" {
		t.Errorf("PS-011 detail drill-down wrong: %+v", got.Details)
	}
	if got := byID["PS-002"]; len(got.Details) != 0 {
		t.Errorf("clean item must carry no finding details, got %+v", got.Details)
	}
	if dto.Framework != "KISA" {
		t.Errorf("report framework = %q, want KISA (default)", dto.Framework)
	}
	if got := byID["PS-011"]; got.Title != ".env 노출" || len(got.CWEs) != 1 || got.CWEs[0] != "CWE-538" {
		t.Errorf("PS-011 catalog/CWE join wrong: %+v", got)
	}
	if got := byID["PS-002"]; got.Result != resultClean {
		t.Errorf("PS-002 (checked, no finding) want clean, got %s", got.Result)
	}
	if got := byID["PS-005"]; got.Result != resultDelegated || got.DelegateTo != "Nuclei" {
		t.Errorf("PS-005 want delegated→Nuclei, got %s→%s", got.Result, got.DelegateTo)
	}
	if got := byID["PS-023"]; got.Result != resultNotApplicable || !got.ActiveHint {
		t.Errorf("PS-023 want not_applicable+activeHint, got %s hint=%v", got.Result, got.ActiveHint)
	}
	if got := byID["PS-002"]; got.Title != "PS-002" {
		t.Errorf("missing catalog entry must fall back to bare PS-ID, got %q", got.Title)
	}

	s := dto.Summary
	if s.Total != 5 || s.Vulnerable != 1 || s.Clean != 1 || s.Delegated != 1 || s.NotApplicable != 1 || s.ToolFailed != 1 {
		t.Errorf("summary tallies wrong: %+v", s)
	}
	if s.Findings != 1 || s.BySeverity["critical"] != 1 {
		t.Errorf("severity histogram wrong: findings=%d bySev=%v", s.Findings, s.BySeverity)
	}
}
