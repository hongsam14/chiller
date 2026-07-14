package probe

import "github.com/hongsam14/chiller/internal/domain"

// Indicative CWE ownership per probe (authoritative rows pinned at C-5, kept in
// sync with configs/mappings/kisa.yaml). Distinct CWEs — no two checkers share a
// CWE, so ownership → PS-ID is unambiguous.
var (
	// humble covers 5 header/cookie items, one CWE each.
	cwesHumble = []domain.CWE{"CWE-319", "CWE-614", "CWE-1004", "CWE-1275", "CWE-1021"} // PS-001/015/016/017/024
	cwesHttpx  = []domain.CWE{"CWE-693"}                                                // PS-007, PS-020 (version)
	cwesTLS    = []domain.CWE{"CWE-1327"}                                               // PS-002
	cwesDir    = []domain.CWE{"CWE-548"}                                                // PS-004
	cwesDefCr  = []domain.CWE{"CWE-798"}                                                // PS-019
	cwesCORS   = []domain.CWE{"CWE-346"}                                                // PS-021 (Origin validation)
	cwesSubzy  = []domain.CWE{domain.NoCanonicalCWE}                                    // PS-025 (attacker axis, ATT&CK)
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

// The 7 probe adapters (r5 composition). URL-scoped: humble, httpx, Corsy,
// changeme. Host-scoped: testssl, dirhunt, subzy. Six are wired this increment;
// changeme stays unwired (Python2 heritage + active credential-testing +
// uncertain JSON schema — wired in a later increment, infra-plan §3).
func Humble() Adapter {
	return newWired("probe:humble", "humble", domain.URLScoped, humbleArgv, humbleParse, cwesHumble...)
}
func Httpx() Adapter {
	return newWired("probe:httpx", "httpx", domain.URLScoped, httpxArgv, httpxParse, cwesHttpx...)
}
func Corsy() Adapter {
	return newWired("probe:corsy", "corsy", domain.URLScoped, corsyArgv, corsyParse, cwesCORS...)
}

// Changeme is deliberately NOT wired this increment (F5): its items (PS-019)
// are honestly reported tool-failed until the Python2/schema questions are
// resolved. Keeping it on newStub also keeps the honesty seam exercised.
func Changeme() Adapter { return newStub("probe:changeme", "changeme", domain.URLScoped, cwesDefCr...) }

// Testssl inspects the target's own TLS handshake, so it must reach the origin
// directly — routing it through the authenticated application-layer proxy is
// meaningless (PR-12c). It is the only probe that declares DirectOnly; every
// other checker keeps the ProxyEligible default.
func Testssl() Adapter {
	r := newWired("probe:testssl", "testssl.sh", domain.HostScoped, testsslArgv, testsslParse, cwesTLS...)
	r.transport = domain.DirectOnly
	return r
}
func Dirhunt() Adapter {
	return newWired("probe:dirhunt", "dirhunt", domain.HostScoped, dirhuntArgv, dirhuntParse, cwesDir...)
}
func Subzy() Adapter {
	return newWired("probe:subzy", "subzy", domain.HostScoped, subzyArgv, subzyParse, cwesSubzy...)
}

// Default returns all 7 probe adapters.
func Default() []Adapter {
	return []Adapter{Humble(), Httpx(), Corsy(), Changeme(), Testssl(), Dirhunt(), Subzy()}
}
