package domain

import (
	"strconv"
	"strings"
)

// CWE is a weakness identifier in canonical MITRE string form, e.g. "CWE-540"
// (Q2). CWE is a finding's canonical identity (the expression axis, FR-14);
// PS-ID lives only on Coverage (the accounting axis).
type CWE string

// NoCanonicalCWE is a reserved sentinel a checker may emit when a check has no
// clean canonical CWE (e.g. PS-025 Subdomain Takeover, assumption A-3). It is a
// valid, explicit value — never an invented CWE (FR-14). The projection layer
// (findings unit) handles it explicitly.
const NoCanonicalCWE CWE = "no-canonical-cwe"

// numericID returns the integer after "CWE-" for ordering, and ok=false for the
// sentinel or any non-canonical form (those sort after real CWEs, deterministically).
func (c CWE) numericID() (int, bool) {
	const prefix = "CWE-"
	if !strings.HasPrefix(string(c), prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(string(c)[len(prefix):])
	if err != nil {
		return 0, false
	}
	return n, true
}
