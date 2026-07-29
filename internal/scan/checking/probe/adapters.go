package probe

import (
	"time"

	"github.com/hongsam14/chiller/internal/domain"
)

// Indicative CWE ownership per probe (authoritative rows pinned at C-5, kept in
// sync with configs/mappings/kisa.yaml). Distinct CWEs — no two checkers share a
// CWE, so ownership → PS-ID is unambiguous.
var (
	// humble analyses response security HEADERS only (not cookies), so it owns
	// just HSTS (PS-001) + clickjacking (PS-024). Cookie flag items PS-015/016/017
	// (CWE-614/1004/1275) moved to the observation Set-Cookie signatures (#0d).
	cwesHumble = []domain.CWE{"CWE-319", "CWE-1021"} // PS-001, PS-024
	cwesHttpx  = []domain.CWE{"CWE-693"}             // PS-007, PS-020 (version)
	cwesTLS    = []domain.CWE{"CWE-1327"}            // PS-002
	cwesDir    = []domain.CWE{"CWE-548"}             // PS-004
	cwesDefCr  = []domain.CWE{"CWE-798"}             // PS-019
	cwesCORS   = []domain.CWE{"CWE-346"}             // PS-021 (Origin validation)
	cwesSubzy  = []domain.CWE{domain.NoCanonicalCWE} // PS-025 (attacker axis, ATT&CK)
)

// stubParse backs an unwired adapter (no real parser yet). Still referenced by
// changeme (below), which is deliberately left unwired this increment.
func stubParse(_ domain.Target, _ []byte) ([]domain.Finding, error) { return nil, nil }

// newStub builds an unwired adapter: it declares its identity/scope/CWEs — so
// boot completeness still sees an owner for every item — but Check fails with
// ErrNotWired, putting its items in StatusToolFailed ("no verdict") rather than
// StatusChecked. Coverage stays a valid 26-partition and stays HONEST: these
// items are reported as unjudged until the real argv/parser/binary land.
func newStub(id domain.CheckerID, tool string, scope domain.ScopeKind, cwes ...domain.CWE) Adapter {
	return &runner{id: id, tool: tool, version: "pinned@build", scope: scope, cwes: cwes, argv: nil, parse: stubParse}
}

// newWired builds a real adapter: argv is non-nil (so Wired() is true) and a real
// stdout→Finding parser is attached. Its items move from tool-failed to checked
// once the pinned binary is present and runs (in a plain `go test` with no binary
// installed, exec still fails → tool-failed, preserving the honesty invariant).
func newWired(id domain.CheckerID, tool string, scope domain.ScopeKind,
	argv func(domain.Target) []string, parse Parser, cwes ...domain.CWE) *runner {
	return &runner{id: id, tool: tool, version: "pinned@build", scope: scope, cwes: cwes, argv: argv, parse: parse}
}

// newWiredFile builds a real adapter for a FILE-output tool: the tool writes its
// JSON to a temp file (outArg injects the path) and the parser reads that file.
// Most probes work this way — only httpx streams JSON to stdout (discovered by
// the first in-image run, F6).
func newWiredFile(id domain.CheckerID, tool string, scope domain.ScopeKind,
	outArg func(domain.Target, string) []string, parse Parser, cwes ...domain.CWE) *runner {
	return &runner{id: id, tool: tool, version: "pinned@build", scope: scope, cwes: cwes, outArg: outArg, parse: parse}
}

// The 7 probe adapters (r5 composition). URL-scoped: humble, httpx, Corsy,
// changeme. Host-scoped: testssl, dirhunt, subzy. Six are wired; changeme stays
// unwired — its PS-019 items are honestly tool-failed (see Changeme below).
func Humble() Adapter {
	return newWiredFile("probe:humble", "humble", domain.URLScoped, humbleOutArg, humbleParse, cwesHumble...)
}
func Httpx() Adapter {
	return newWired("probe:httpx", "httpx", domain.URLScoped, httpxArgv, httpxParse, cwesHttpx...)
}
func Corsy() Adapter {
	return newWiredFile("probe:corsy", "corsy", domain.URLScoped, corsyOutArg, corsyParse, cwesCORS...)
}

// Changeme (ztgrace/changeme) is deliberately NOT wired: its PS-019 items are
// honestly reported tool-failed. An r19 Docker smoke of v1.2.3 (the accurate
// reason, superseding the earlier "Python2 heritage" note — the source is in
// fact Python3) found it a poor fit for the hardened-minimal image:
//   - it eager-imports every protocol scanner, incl. telnet → `telnetlib`, which
//     PEP 594 REMOVED in Python 3.13+; on the python:3.14 base it will not import
//     without a stdlib shim or pinning the runtime back to Python ≤3.12;
//   - its deps (paramiko/pysnmp/DB drivers/selenium+phantomjs/shodan, all
//     eager-imported) add ~1 GB and a large attack surface — at odds with
//     SECURITY-09 (minimal image) and the non-root/read-only-rootfs hardening.
//
// The tool IS functionally bound-able (--protocols http default, --name to narrow
// creds, --fingerprint as an applicability oracle, JSON {"results":[…]} output,
// creds baked in-repo), so the block is packaging fit, not capability. Revisit if
// upstream drops the telnet eager-import and slims deps, or via a purpose-built
// minimal HTTP default-cred check. Keeping it on newStub keeps the honesty seam
// exercised (PS-019 stays an honest tool-failed, never a fabricated clean).
func Changeme() Adapter { return newStub("probe:changeme", "changeme", domain.URLScoped, cwesDefCr...) }

// Testssl inspects the target's own TLS handshake, so it must reach the origin
// directly — routing it through the authenticated application-layer proxy is
// meaningless (PR-12c). It is the only probe that declares DirectOnly; every
// other checker keeps the ProxyEligible default.
func Testssl() Adapter {
	r := newWiredFile("probe:testssl", "testssl.sh", domain.HostScoped, testsslOutArg, testsslParse, cwesTLS...)
	r.transport = domain.DirectOnly
	r.timeout = 180 * time.Second // full TLS enumeration is slow (many handshakes)
	return r
}
func Dirhunt() Adapter {
	r := newWiredFile("probe:dirhunt", "dirhunt", domain.HostScoped, dirhuntOutArg, dirhuntParse, cwesDir...)
	r.timeout = 180 * time.Second // dirhunt crawls; the 30s default kills it mid-crawl
	return r
}
func Subzy() Adapter {
	r := newWiredFile("probe:subzy", "subzy", domain.HostScoped, subzyOutArg, subzyParse, cwesSubzy...)
	r.precheck = subzyDBGuard // #0e: fail-fast tool-failed if the DB is absent/empty (never false-clean)
	r.extraEnv = subzyRunEnv  // #0e: HOME→ subzy reads the mounted DB instead of self-fetching
	return r
}

// Default returns all 7 probe adapters.
func Default() []Adapter {
	return []Adapter{Humble(), Httpx(), Corsy(), Changeme(), Testssl(), Dirhunt(), Subzy()}
}
