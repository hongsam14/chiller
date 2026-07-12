package domain

// TransportKind declares how a checker's outbound traffic must egress when the
// scan runs in financial (authenticated) mode (PR-12c). It is data the checker
// carries, not a branch in the orchestrator: the composition root reads it to
// decide whether a checker routes through the single authenticated auth-proxy or
// goes out directly.
type TransportKind int

const (
	// ProxyEligible (the zero value, hence the safe default) means the checker's
	// requests are ordinary application-layer traffic that MUST carry the
	// authenticated browser session in financial mode — so they route through the
	// auth-proxy. Defaulting here is fail-safe: a checker that forgets to declare
	// its transport routes authenticated (correct results) rather than hitting the
	// target unauthenticated (wrong results).
	ProxyEligible TransportKind = iota
	// DirectOnly means the checker inspects a layer where an application-session
	// proxy is meaningless or harmful — e.g. testssl.sh, which tests the target's
	// own TLS handshake and must reach the origin directly, not a MITM proxy.
	DirectOnly
)

// TransportAware is an optional capability (same pattern as the registry's
// `owned` interface): a checker that implements it declares its TransportKind.
// A checker that does NOT implement it is treated as ProxyEligible (the
// fail-safe default), so existing checkers need no change.
type TransportAware interface {
	Transport() TransportKind
}

// TransportOf reports a checker's transport policy, applying the ProxyEligible
// default to checkers that do not implement TransportAware.
func TransportOf(c Checker) TransportKind {
	if ta, ok := c.(TransportAware); ok {
		return ta.Transport()
	}
	return ProxyEligible
}
