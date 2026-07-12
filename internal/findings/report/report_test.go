package report

import (
	"strings"
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/findings/compliance"
	"github.com/enki-offen/chiller/internal/findings/config"
)

func TestBuild_TwoAxesAndLabels(t *testing.T) {
	mt, err := config.LoadMapping(strings.NewReader("version: \"r1\"\nmap:\n  CWE-615: { KISA: [PS-009] }\n"))
	if err != nil {
		t.Fatal(err)
	}
	b := New(compliance.New(mt))

	findings := []domain.Finding{{CWEs: []domain.CWE{"CWE-615"}, TargetURL: "u", SourceTool: "observation:CWE-615", Severity: domain.SevMedium}}
	cov := domain.Coverage{"PS-009": {Status: domain.StatusChecked}}

	r := b.Build("scan-1", findings, cov, []domain.Framework{domain.FrameworkKISA})
	if r.MappingRevision != "r1" {
		t.Errorf("revision=%q", r.MappingRevision)
	}
	if len(r.Findings) != 1 || len(r.Coverage) != 1 {
		t.Error("both axes must be present")
	}
	idx := r.FrameworkLabels[domain.FrameworkKISA]["PS-009"]
	if len(idx) != 1 || idx[0] != 0 {
		t.Errorf("PS-009 label should point at finding 0, got %v", idx)
	}
}
