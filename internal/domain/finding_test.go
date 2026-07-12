package domain

import (
	"reflect"
	"testing"
)

func TestNormalizeCWEs_SortDedup(t *testing.T) {
	in := []CWE{"CWE-540", "CWE-79", "CWE-540", "CWE-200"}
	want := []CWE{"CWE-79", "CWE-200", "CWE-540"}
	if got := NormalizeCWEs(in); !reflect.DeepEqual(got, want) {
		t.Errorf("NormalizeCWEs(%v)=%v, want %v", in, got, want)
	}
}

func TestNormalizeCWEs_Idempotent(t *testing.T) {
	// R1 property: normalize∘normalize = normalize.
	inputs := [][]CWE{
		{"CWE-540", "CWE-79", "CWE-540"},
		{NoCanonicalCWE},
		{"CWE-79", NoCanonicalCWE, "CWE-200"}, // sentinel dropped when real CWEs present
		{},
	}
	for _, in := range inputs {
		once := NormalizeCWEs(in)
		twice := NormalizeCWEs(once)
		if !reflect.DeepEqual(once, twice) {
			t.Errorf("not idempotent for %v: once=%v twice=%v", in, once, twice)
		}
	}
}

func TestNormalizeCWEs_SentinelExclusivity(t *testing.T) {
	// Real CWE present ⇒ sentinel dropped.
	if got := NormalizeCWEs([]CWE{NoCanonicalCWE, "CWE-200"}); !reflect.DeepEqual(got, []CWE{"CWE-200"}) {
		t.Errorf("expected sentinel dropped when real CWE present, got %v", got)
	}
	// Only sentinel ⇒ exactly [NoCanonicalCWE].
	if got := NormalizeCWEs([]CWE{NoCanonicalCWE, NoCanonicalCWE}); !reflect.DeepEqual(got, []CWE{NoCanonicalCWE}) {
		t.Errorf("expected [NoCanonicalCWE], got %v", got)
	}
}

func TestNormalizeCWEs_DoesNotMutateInput(t *testing.T) {
	in := []CWE{"CWE-540", "CWE-79"}
	cp := append([]CWE(nil), in...)
	_ = NormalizeCWEs(in)
	if !reflect.DeepEqual(in, cp) {
		t.Errorf("NormalizeCWEs mutated its input: %v", in)
	}
}

func TestIdentityKey_StableUnderReorder(t *testing.T) {
	// R2: identity is (normalized CWEs, TargetURL, SourceTool) — order-independent in CWEs.
	a := Finding{CWEs: []CWE{"CWE-540", "CWE-79"}, TargetURL: "https://x/y", SourceTool: "observation:CWE-615"}
	b := Finding{CWEs: []CWE{"CWE-79", "CWE-540"}, TargetURL: "https://x/y", SourceTool: "observation:CWE-615"}
	if a.IdentityKey() != b.IdentityKey() {
		t.Errorf("IdentityKey not stable under CWE reorder: %q vs %q", a.IdentityKey(), b.IdentityKey())
	}
}

func TestNormalizeCWEs_NonNumericFallback(t *testing.T) {
	// Non-canonical forms sort after real CWEs, deterministically (string fallback).
	in := []CWE{"CWE-zzz", "CWE-79", "CWE-aaa"}
	got := NormalizeCWEs(in)
	want := []CWE{"CWE-79", "CWE-aaa", "CWE-zzz"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NormalizeCWEs(%v)=%v, want %v", in, got, want)
	}
	// Idempotent on this input too.
	if !reflect.DeepEqual(NormalizeCWEs(got), got) {
		t.Errorf("non-numeric fallback not idempotent")
	}
}

func TestFindingValidate(t *testing.T) {
	valid := Finding{CWEs: []CWE{"CWE-540"}, TargetURL: "https://x/y", SourceTool: "httpx", Severity: SevLow}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid finding, got %v", err)
	}
	sentinelOnly := Finding{CWEs: []CWE{NoCanonicalCWE}, TargetURL: "https://x", SourceTool: "subzy", Severity: SevInfo}
	if err := sentinelOnly.Validate(); err != nil {
		t.Errorf("sentinel-only finding should be valid, got %v", err)
	}

	bad := []struct {
		name string
		f    Finding
	}{
		{"no CWEs", Finding{CWEs: nil, TargetURL: "u", SourceTool: "t", Severity: SevInfo}},
		{"empty CWE slice", Finding{CWEs: []CWE{}, TargetURL: "u", SourceTool: "t", Severity: SevInfo}},
		{"invalid severity", Finding{CWEs: []CWE{"CWE-1"}, TargetURL: "u", SourceTool: "t", Severity: Severity(99)}},
		{"empty URL", Finding{CWEs: []CWE{"CWE-1"}, TargetURL: "", SourceTool: "t", Severity: SevInfo}},
		{"empty tool", Finding{CWEs: []CWE{"CWE-1"}, TargetURL: "u", SourceTool: "", Severity: SevInfo}},
	}
	for _, c := range bad {
		if err := c.f.Validate(); err == nil {
			t.Errorf("%s: expected validation error", c.name)
		}
	}
}

func TestIdentityKey_DistinguishesToolAndURL(t *testing.T) {
	base := Finding{CWEs: []CWE{"CWE-540"}, TargetURL: "https://x/y", SourceTool: "httpx"}
	otherTool := base
	otherTool.SourceTool = "humble"
	otherURL := base
	otherURL.TargetURL = "https://x/z"
	if base.IdentityKey() == otherTool.IdentityKey() {
		t.Error("different SourceTool should yield different identity (Q1=B tool-scoped)")
	}
	if base.IdentityKey() == otherURL.IdentityKey() {
		t.Error("different TargetURL should yield different identity")
	}
}
