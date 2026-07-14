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

// stubParse is used until a tool's real output parser is authored (C-5).
func stubParse(_ domain.Target, _ []byte) ([]domain.Finding, error) { return nil, nil }

// newStub builds an adapter whose binary isn't wired yet: it declares its
// identity/scope/CWEs and, when Check runs, reports nothing (Checked, no finding)
// — so coverage stays complete while the real parser/binary land at C-5/Infra.
func newStub(id domain.CheckerID, tool string, scope domain.ScopeKind, cwes ...domain.CWE) Adapter {
	return &runner{id: id, tool: tool, version: "pinned@build", scope: scope, cwes: cwes, argv: nil, parse: stubParse}
}

// The 7 probe adapters (r5 composition). URL-scoped: humble, httpx, Corsy,
// changeme. Host-scoped: testssl, dirhunt, subzy.
func Humble() Adapter   { return newStub("probe:humble", "humble", domain.URLScoped, cwesHumble...) }
func Httpx() Adapter    { return newStub("probe:httpx", "httpx", domain.URLScoped, cwesHttpx...) }
func Corsy() Adapter    { return newStub("probe:corsy", "corsy", domain.URLScoped, cwesCORS...) }
func Changeme() Adapter { return newStub("probe:changeme", "changeme", domain.URLScoped, cwesDefCr...) }

// Testssl inspects the target's own TLS handshake, so it must reach the origin
// directly — routing it through the authenticated application-layer proxy is
// meaningless (PR-12c). It is the only probe that declares DirectOnly; every
// other checker keeps the ProxyEligible default.
func Testssl() Adapter {
	r := newStub("probe:testssl", "testssl.sh", domain.HostScoped, cwesTLS...).(*runner)
	r.transport = domain.DirectOnly
	return r
}
func Dirhunt() Adapter { return newStub("probe:dirhunt", "dirhunt", domain.HostScoped, cwesDir...) }
func Subzy() Adapter   { return newStub("probe:subzy", "subzy", domain.HostScoped, cwesSubzy...) }

// Default returns all 7 probe adapters.
func Default() []Adapter {
	return []Adapter{Humble(), Httpx(), Corsy(), Changeme(), Testssl(), Dirhunt(), Subzy()}
}
