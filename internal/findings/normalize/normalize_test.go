package normalize

import (
	"math/rand"
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
)

type fakeSev struct{}

func (fakeSev) Map(raw, tool string) domain.Severity {
	if raw == "high" {
		return domain.SevHigh
	}
	return domain.SevInfo
}

func TestNormalize_OK(t *testing.T) {
	n := New(fakeSev{})
	f, err := n.Normalize(RawFinding{CWEs: []domain.CWE{"CWE-540", "CWE-79"}, TargetURL: "u", SourceTool: "httpx", RawSeverity: "high", Evidence: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if f.Severity != domain.SevHigh || f.SourceTool != "httpx" {
		t.Errorf("normalized wrong: %+v", f)
	}
	// CWEs normalized (sorted).
	if f.CWEs[0] != "CWE-79" {
		t.Errorf("CWEs not normalized: %v", f.CWEs)
	}
}

func TestNormalize_InvalidReturnsError(t *testing.T) {
	n := New(fakeSev{})
	if _, err := n.Normalize(RawFinding{CWEs: nil, TargetURL: "u", SourceTool: "httpx"}); err == nil {
		t.Error("no CWEs should be an error, not a panic")
	}
}

func TestNormalize_EvidenceBounded(t *testing.T) {
	n := New(fakeSev{})
	many := make([]string, 100)
	for i := range many {
		many[i] = "s"
	}
	f, err := n.Normalize(RawFinding{CWEs: []domain.CWE{"CWE-1"}, TargetURL: "u", SourceTool: "t", Evidence: many})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Evidence.Snippets) != domain.MaxEvidenceSnippets {
		t.Errorf("evidence not bounded: %d", len(f.Evidence.Snippets))
	}
}

// P1: normalization is total — never panics, even on garbage; invalid drops.
func TestNormalize_Property_Total(t *testing.T) {
	n := New(fakeSev{})
	rng := rand.New(rand.NewSource(2))
	cwePool := []domain.CWE{"CWE-1", "CWE-540", domain.NoCanonicalCWE, "", "garbage"}
	for range 500 {
		raw := RawFinding{
			CWEs:       []domain.CWE{cwePool[rng.Intn(len(cwePool))]},
			TargetURL:  []string{"", "u"}[rng.Intn(2)],
			SourceTool: []string{"", "t"}[rng.Intn(2)],
		}
		if rng.Intn(2) == 0 {
			raw.CWEs = nil
		}
		_, _ = n.Normalize(raw) // must not panic; err is fine
	}
	// batch path returns a dropped count, never crashes
	out, dropped := n.NormalizeAll([]RawFinding{
		{CWEs: []domain.CWE{"CWE-1"}, TargetURL: "u", SourceTool: "t"},
		{CWEs: nil}, // invalid → dropped
	})
	if len(out) != 1 || dropped != 1 {
		t.Errorf("NormalizeAll out=%d dropped=%d, want 1/1", len(out), dropped)
	}
}
