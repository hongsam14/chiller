package domain

import "strings"

// Severity is an ordinal severity scale (business-rule R3/R4).
// Info < Low < Medium < High < Critical, so comparison is meaningful.
type Severity int

const (
	// SevUnset is the zero value and is never Valid. Like StatusUnset, it exists
	// so that an absent field cannot masquerade as a real value: a BSON decoder
	// leaves the Go zero value in place for a field the document does not contain,
	// so with SevInfo at zero a finding that lost its severity would quietly read
	// as informational — a silent downgrade. Finding.Validate (R4) now rejects it.
	SevUnset Severity = iota
	SevInfo
	SevLow
	SevMedium
	SevHigh
	SevCritical
)

// severityNames backs String and bounds-checks validity. These are also the
// PERSISTED and wire forms (G2): a Severity is stored as its name, never as the
// iota ordinal, so inserting or reordering a constant cannot silently reinterpret
// existing documents.
// Index 0 (SevUnset) deliberately has no name, so it can neither be written nor
// parsed.
var severityNames = [...]string{"", "info", "low", "medium", "high", "critical"}

// String returns the canonical name, or "unknown" for the unset/out-of-range values.
func (s Severity) String() string {
	if !s.Valid() {
		return "unknown"
	}
	return severityNames[s]
}

// ParseSeverity is the inverse of String, case-insensitively (config files write
// "Critical", "CRITICAL" and "critical" interchangeably). An unknown or empty
// name is rejected rather than defaulted — a silent fallback would turn a typo in
// a config file into a downgraded finding.
func ParseSeverity(name string) (Severity, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return SevUnset, false
	}
	for i, n := range severityNames {
		if n == lower {
			return Severity(i), true
		}
	}
	return SevUnset, false
}

// Valid reports whether s is one of the defined levels. MapSeverity (findings
// unit) is a total function (R4) and must only ever produce Valid severities;
// unmappable tool severities default to SevInfo (Q3b — passive/recon tier).
func (s Severity) Valid() bool {
	return s >= SevInfo && int(s) < len(severityNames)
}

// Max returns the higher of two severities (R3: used when collapsing duplicate
// findings emitted by the same tool for the same identity key).
func Max(a, b Severity) Severity {
	if a >= b {
		return a
	}
	return b
}
