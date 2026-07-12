package domain

import "testing"

func TestSeverityOrdering(t *testing.T) {
	// Info < Low < Medium < High < Critical (R3/R4).
	ordered := []Severity{SevInfo, SevLow, SevMedium, SevHigh, SevCritical}
	for i := 1; i < len(ordered); i++ {
		if !(ordered[i-1] < ordered[i]) {
			t.Fatalf("severity not strictly increasing at %d: %v !< %v", i, ordered[i-1], ordered[i])
		}
	}
}

func TestSeverityValid(t *testing.T) {
	for _, s := range []Severity{SevInfo, SevLow, SevMedium, SevHigh, SevCritical} {
		if !s.Valid() {
			t.Errorf("%v should be Valid", s)
		}
	}
	for _, s := range []Severity{-1, 5, 99} {
		if s.Valid() {
			t.Errorf("%d should be invalid", s)
		}
		if s.String() != "Unknown" {
			t.Errorf("invalid severity %d String()=%q, want Unknown", s, s.String())
		}
	}
}

func TestSeverityMax(t *testing.T) {
	cases := []struct{ a, b, want Severity }{
		{SevInfo, SevCritical, SevCritical},
		{SevHigh, SevLow, SevHigh},
		{SevMedium, SevMedium, SevMedium},
		{SevCritical, SevCritical, SevCritical},
	}
	for _, c := range cases {
		if got := Max(c.a, c.b); got != c.want {
			t.Errorf("Max(%v,%v)=%v, want %v", c.a, c.b, got, c.want)
		}
		if got := Max(c.b, c.a); got != c.want {
			t.Errorf("Max is not commutative for (%v,%v)", c.a, c.b)
		}
	}
}
