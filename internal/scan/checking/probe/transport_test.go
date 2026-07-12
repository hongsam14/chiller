package probe

import (
	"context"
	"strings"
	"testing"

	"github.com/enki-offen/chiller/internal/domain"
)

// testssl inspects the TLS handshake, so it must reach the origin directly:
// DirectOnly. Every other probe keeps the ProxyEligible default so it egresses
// through the authenticated auth-proxy in financial mode (PR-12c).
func TestProbeTransport_Declarations(t *testing.T) {
	cases := map[domain.CheckerID]domain.TransportKind{
		"probe:testssl":  domain.DirectOnly,
		"probe:humble":   domain.ProxyEligible,
		"probe:httpx":    domain.ProxyEligible,
		"probe:corsy":    domain.ProxyEligible,
		"probe:changeme": domain.ProxyEligible,
		"probe:dirhunt":  domain.ProxyEligible,
		"probe:subzy":    domain.ProxyEligible,
	}
	byID := map[domain.CheckerID]Adapter{}
	for _, a := range Default() {
		byID[a.ID()] = a
	}
	for id, want := range cases {
		a, ok := byID[id]
		if !ok {
			t.Fatalf("adapter %q missing from Default()", id)
		}
		if got := domain.TransportOf(a); got != want {
			t.Errorf("%s transport = %v, want %v", id, got, want)
		}
	}
}

// Every probe implements TransportAware (via the runner), so transport is a
// first-class declared attribute, not an orchestrator branch.
func TestProbeTransport_AllAware(t *testing.T) {
	for _, a := range Default() {
		if _, ok := a.(domain.TransportAware); !ok {
			t.Errorf("%s does not declare TransportAware", a.ID())
		}
	}
}

// ConfigureProxy injects HTTP(S)_PROXY into ProxyEligible adapters only; testssl
// (DirectOnly) is left direct (PR-12c). Data-driven — no per-tool special-casing.
func TestConfigureProxy_TransportDriven(t *testing.T) {
	adapters := Default()
	ConfigureProxy(adapters, "http://127.0.0.1:8889")

	for _, a := range adapters {
		r := a.(*runner)
		hasProxy := len(r.proxyEnv) > 0
		if a.ID() == "probe:testssl" {
			if hasProxy {
				t.Errorf("testssl (DirectOnly) must NOT get proxy env, got %v", r.proxyEnv)
			}
			continue
		}
		if !hasProxy {
			t.Errorf("%s (ProxyEligible) should get proxy env, got none", a.ID())
		}
	}

	// Empty proxyURL is a no-op.
	fresh := Default()
	ConfigureProxy(fresh, "")
	if len(fresh[0].(*runner).proxyEnv) != 0 {
		t.Error("empty proxyURL must not set any env")
	}
}

// The injected proxy env actually reaches the subprocess exec for a ProxyEligible
// adapter, and is absent for a DirectOnly one.
func TestConfigureProxy_EnvReachesExec(t *testing.T) {
	var gotEnv []string
	capture := func(_ context.Context, _ string, env []string, _ ...string) ([]byte, error) {
		gotEnv = env
		return []byte("ok"), nil
	}

	proxyEligible := &runner{
		id: "probe:pe", tool: "pe", transport: domain.ProxyEligible,
		argv: func(domain.Target) []string { return []string{"x"} }, parse: stubParse, exec: capture,
	}
	directOnly := &runner{
		id: "probe:do", tool: "do", transport: domain.DirectOnly,
		argv: func(domain.Target) []string { return []string{"x"} }, parse: stubParse, exec: capture,
	}
	ConfigureProxy([]Adapter{proxyEligible, directOnly}, "http://127.0.0.1:8889")

	gotEnv = nil
	_, _ = proxyEligible.Check(domain.ScanContext{Target: domain.Target{URL: "https://a/"}})
	if !hasEnv(gotEnv, "HTTP_PROXY=http://127.0.0.1:8889") {
		t.Errorf("ProxyEligible exec env = %v, want HTTP_PROXY set", gotEnv)
	}

	gotEnv = nil
	_, _ = directOnly.Check(domain.ScanContext{Target: domain.Target{URL: "https://a/"}})
	if len(gotEnv) != 0 {
		t.Errorf("DirectOnly exec env = %v, want none", gotEnv)
	}
}

func hasEnv(env []string, kv string) bool {
	for _, e := range env {
		if strings.EqualFold(e, kv) {
			return true
		}
	}
	return false
}
