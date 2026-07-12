package store

import (
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
)

func TestInMemory_RoundTrip(t *testing.T) {
	s := NewInMemory()
	fs := []domain.Finding{{CWEs: []domain.CWE{"CWE-1"}, TargetURL: "u", SourceTool: "t", Severity: domain.SevLow}}
	if err := s.SaveFindings("scan-1", fs); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadFindings("scan-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SourceTool != "t" {
		t.Errorf("round-trip wrong: %+v", got)
	}
	// Isolation: caller mutating its slice must not affect the store.
	fs[0].SourceTool = "mutated"
	again, _ := s.LoadFindings("scan-1")
	if again[0].SourceTool != "t" {
		t.Error("store did not copy on save")
	}
}

func TestInMemory_MissingScan(t *testing.T) {
	s := NewInMemory()
	got, err := s.LoadFindings("nope")
	if err != nil || len(got) != 0 {
		t.Errorf("missing scan should be empty, got %v err %v", got, err)
	}
}
