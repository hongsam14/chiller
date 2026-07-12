package domain

// Severity is an ordinal severity scale (business-rule R3/R4).
// Info < Low < Medium < High < Critical, so comparison is meaningful.
type Severity int

const (
	SevInfo Severity = iota
	SevLow
	SevMedium
	SevHigh
	SevCritical
)

// severityNames backs String and bounds-checks validity.
var severityNames = [...]string{"Info", "Low", "Medium", "High", "Critical"}

// String returns the human-readable name, or "Unknown" for out-of-range values.
func (s Severity) String() string {
	if s < SevInfo || int(s) >= len(severityNames) {
		return "Unknown"
	}
	return severityNames[s]
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
