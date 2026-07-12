package domain

// ScopeKind classifies a checker by its unit of operation (constraint C-6).
type ScopeKind int

const (
	// URLScoped checkers judge a single URL's fetched response (shared via the
	// ResponseCache, FR-02): the Observation engine, humble, httpx, Corsy, changeme.
	URLScoped ScopeKind = iota
	// HostScoped checkers operate once per host and do their own protocol I/O:
	// testssl, dirhunt, subzy.
	HostScoped
)

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
