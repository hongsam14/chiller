package domain

// ScopeKind classifies a checker by its unit of operation (constraint C-6).
type ScopeKind int

const (
	// ScopeUnset is the zero value and is never Valid — for the same reason
	// StatusUnset and SevUnset exist. A WorkItem's scope decides which checker set
	// runs on it, and a decoder leaves the Go zero value in place for a field the
	// document does not contain. With URLScoped at zero, an item that lost its
	// scope would run the URL-scoped checkers and the coverage fold would then
	// record the HOST-scoped items (testssl, dirhunt, subzy) as checked when
	// nothing had run them: a compliance false negative.
	ScopeUnset ScopeKind = iota
	// URLScoped checkers judge a single URL's fetched response (shared via the
	// ResponseCache, FR-02): the Observation engine, humble, httpx, Corsy, changeme.
	URLScoped
	// HostScoped checkers operate once per host and do their own protocol I/O:
	// testssl, dirhunt, subzy.
	HostScoped
)

// scopeKindNames are the persisted and wire forms (G2) — a WorkItem's scope is
// written to the durable ledger and to the queue payload, so it must not depend
// on the iota ordinal. Index 0 (ScopeUnset) deliberately has no name.
var scopeKindNames = [...]string{"", "url", "host"}

// Valid reports whether s names a real scope.
func (s ScopeKind) Valid() bool { return s == URLScoped || s == HostScoped }

// String returns the canonical name, or "unknown" for the unset/out-of-range values.
func (s ScopeKind) String() string {
	if !s.Valid() {
		return "unknown"
	}
	return scopeKindNames[s]
}

// ParseScopeKind is the inverse of String. An unknown or empty name is rejected,
// never defaulted — defaulting would silently reassign a host-scoped item to the
// URL-scoped checker set.
func ParseScopeKind(name string) (ScopeKind, bool) {
	if name == "" {
		return ScopeUnset, false
	}
	for i, n := range scopeKindNames {
		if n == name {
			return ScopeKind(i), true
		}
	}
	return ScopeUnset, false
}

// CheckerID uniquely identifies a checker, e.g. "observation:CWE-615" or "probe:humble".
type CheckerID string

// PSID is a compliance-item identifier (KISA "PS-001".."PS-026" in MVP). The
// kernel does not hardcode the item set — the specific items in force are
// supplied per scan via CoverageSpec (from config), so changing the checked
// items is a config change, not a kernel change.
type PSID string

// Framework names a compliance framework a finding's CWEs can be projected onto.
// MVP populates only "KISA"; "OWASP" and "CAPEC" are Post-MVP (FR-14, NFR-15).
type Framework string

const (
	FrameworkKISA  Framework = "KISA"
	FrameworkOWASP Framework = "OWASP"
	FrameworkCAPEC Framework = "CAPEC"
)

// DelegateTarget names the stage a delegated item is covered by (FR-11).
type DelegateTarget string

const (
	DelegateASM    DelegateTarget = "ASM"    // PS-014
	DelegateNuclei DelegateTarget = "Nuclei" // PS-005, PS-006, PS-026
)

// ScanID and WorkItemID identify a scan aggregate and a fan-out work item.
type (
	ScanID     string
	WorkItemID string
)

// Timestamp is unix epoch milliseconds. The domain treats it as opaque; it is
// stamped by whichever unit performs the I/O (e.g. the Fetcher).
type Timestamp int64
