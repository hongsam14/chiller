package domain

// Target is the subject of a check — a host and/or a specific URL.
type Target struct {
	URL    string `bson:"url"`
	Host   string `bson:"host"`
	Scheme string `bson:"scheme"` // "http" | "https"
}

// ScanContext is the only payload the control plane hands a checker (Q4/Q5). It
// carries data, not control: a Target and, for URL-scoped checkers, the shared
// fetched response plus any statically <script src>-discovered first-party JS
// (r4/C-8, approved hosts only). Host-scoped checkers see Response == nil and do
// their own protocol I/O.
type ScanContext struct {
	Target       Target
	Response     *HAREntry   // non-nil for URL-scoped checkers (shared fetch, FR-02)
	DiscoveredJS []*HAREntry // r4/C-8: statically discovered first-party JS (may be empty)
}

// CheckResult is a checker's successful output (FQ5): the findings it produced,
// plus the CWEs it judged structurally not-applicable to this target. N/A is a
// first-class result — distinct from an error (which yields tool-failed) and
// from a finding (an observed issue).
type CheckResult struct {
	Findings      []Finding
	NotApplicable []CWE
}

// Checker is the plugin seam (NFR-02): both checker families — the in-process
// Observation engine and each external-CLI Probe adapter — implement it, so the
// orchestrator sees only this interface and a new checker registers without any
// orchestrator change.
//
// Check is all-or-nothing (R7, Q4): a CheckResult on success, or an error whose
// owned item(s) then become StatusToolFailed for the scan. (The ObservationEngine
// runs its signatures independently and aggregates per-signature outcomes, so one
// failing signature fails only its own item.)
type Checker interface {
	Check(ScanContext) (CheckResult, error)
	Scope() ScopeKind
	ID() CheckerID
}
