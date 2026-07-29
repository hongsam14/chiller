//go:build integration

// Integration proof for the wired probes (Q4=C integration part, IQ4=A). Runs
// ONLY under `-tags integration` inside the built probe image, where the pinned
// tool binaries actually exist. It exercises the REAL defaultExec (os/exec) path
// against each wired tool and feeds the tool's REAL stdout to its parser — this
// is the point at which the hand-authored golden schemas (F6) are first checked
// against reality. A schema drift surfaces here as a parse error; fix the parser
// + golden fixture, never loosen the parser to hide it.
//
// This is the honest half of "coverage rises": it proves tool-ran + output-parsed
// (→ the item can move off tool-failed). Positive-finding parsing is proven by
// the golden-fixture unit tests; reproducing a genuinely-weak target (weak TLS,
// dangling subdomain, dir listing) in CI is out of scope (F4).
package probe

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

// binaryOnPath reports whether a tool is installed; integration steps skip
// (not fail) when their binary is absent, so a partial image is still testable.
func binaryOnPath(tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}

// TestIntegration_URLProbes_RealExecAndParse runs the HTTP URL-scoped probes
// (httpx, humble, corsy) against a local benign server. On a clean target the
// tools should run and their parsers should not error — proving the real
// os/exec + real-output-parse path end to end.
func TestIntegration_URLProbes_RealExecAndParse(t *testing.T) {
	// A deliberately-weak local server: a version banner (httpx → CWE-693), a
	// wildcard ACAO (Corsy → CWE-346), and no HSTS/X-Frame/CSP (humble → CWE-319
	// + CWE-1021). This proves each probe extracts a REAL positive finding from
	// real tool output — not just "runs clean" (#0d).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "nginx/1.27.5")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = w.Write([]byte(`<html><body>ok</body></html>`))
	}))
	defer srv.Close()
	host := srv.Listener.Addr().String()
	target := domain.Target{URL: srv.URL + "/", Host: host, Scheme: "http"}

	want := map[string]domain.CWE{"humble": "CWE-319", "httpx": "CWE-693", "corsy": "CWE-346"}
	for _, a := range []Adapter{Humble(), Httpx(), Corsy()} {
		a := a
		t.Run(a.ToolName(), func(t *testing.T) {
			if !binaryOnPath(a.ToolName()) {
				t.Skipf("%s not installed in this image — skipping", a.ToolName())
			}
			res, err := a.Check(domain.ScanContext{Target: target})
			if err != nil {
				t.Fatalf("%s: real exec+parse errored (schema drift?): %v", a.ToolName(), err)
			}
			// The tool ran against a weak target → it must extract at least one
			// finding, and its owned CWE must appear (real positive extraction).
			wantCWE := want[a.ToolName()]
			found := false
			for _, f := range res.Findings {
				for _, c := range f.CWEs {
					if c == wantCWE {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("%s: expected a %s finding from the weak target, got %d findings %v",
					a.ToolName(), wantCWE, len(res.Findings), res.Findings)
			}
		})
	}
}

// TestIntegration_HostProbes_RealExecAndParse covers the host-scoped probes.
// testssl needs a TLS :443 endpoint, dirhunt a crawlable site, subzy real DNS —
// so they read their target from PROBE_TEST_HOST (e.g. a staging host) and skip
// when it is unset, keeping CI green while still being runnable on demand.
func TestIntegration_HostProbes_RealExecAndParse(t *testing.T) {
	host := os.Getenv("PROBE_TEST_HOST")
	if host == "" {
		t.Skip("PROBE_TEST_HOST unset — host-scoped probe integration needs a real target")
	}
	target := domain.Target{URL: "https://" + host + "/", Host: host, Scheme: "https"}
	for _, a := range []Adapter{Testssl(), Dirhunt(), Subzy()} {
		a := a
		t.Run(a.ToolName(), func(t *testing.T) {
			if !binaryOnPath(a.ToolName()) {
				t.Skipf("%s not installed in this image — skipping", a.ToolName())
			}
			// subzy needs its operator-provisioned fingerprint DB (#0e); without it the
			// precheck legitimately tool-fails, so skip rather than misreport an error.
			if a.ToolName() == "subzy" && subzyDBGuard(domain.ScanContext{}) != nil {
				t.Skip("subzy fingerprint DB not provisioned (set SUBZY_DB / run `make subzy-db`) — skipping")
			}
			if _, err := a.Check(domain.ScanContext{Target: target}); err != nil {
				t.Errorf("%s: real exec+parse errored (schema drift or unreachable target): %v", a.ToolName(), err)
			}
		})
	}
}

// TestIntegration_FixtureProbes_PositiveFindings proves the host-scoped probes
// extract a REAL positive finding from a genuinely-weak target (#0d, r9) — the
// half that the benign no-error check above cannot show. IQ2=A (hybrid): opt-in
// per probe via env, so base CI stays green (the URL probes above already assert
// positives in-process). Point each env at the committed vuln-target fixture
// (dirhunt/subzy — this container must be joined to the fixture's `vulnnet`) or
// public badssl (testssl — needs outbound TLS):
//
//	PROBE_FIXTURE_DIRHUNT=vulnweb        (nginx autoindex → CWE-548)
//	PROBE_FIXTURE_SUBZY=takeover-mock    (takeover fingerprint → NoCanonicalCWE;
//	                                      needs subzy's fingerprints DB present)
//	PROBE_BADSSL_HOST=rc4.badssl.com     (weak TLS → CWE-1327)
func TestIntegration_FixtureProbes_PositiveFindings(t *testing.T) {
	cases := []struct {
		name    string
		env     string
		adapter Adapter
		mk      func(host string) domain.Target
		wantCWE domain.CWE
	}{
		{
			name: "dirhunt", env: "PROBE_FIXTURE_DIRHUNT", adapter: Dirhunt(), wantCWE: "CWE-548",
			mk: func(h string) domain.Target { return domain.Target{URL: "http://" + h + "/", Host: h, Scheme: "http"} },
		},
		{
			name: "subzy", env: "PROBE_FIXTURE_SUBZY", adapter: Subzy(), wantCWE: domain.NoCanonicalCWE,
			mk: func(h string) domain.Target { return domain.Target{URL: "http://" + h + "/", Host: h, Scheme: "http"} },
		},
		{
			name: "testssl.sh", env: "PROBE_BADSSL_HOST", adapter: Testssl(), wantCWE: "CWE-1327",
			mk: func(h string) domain.Target {
				return domain.Target{URL: "https://" + h + "/", Host: h, Scheme: "https"}
			},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			host := os.Getenv(c.env)
			if host == "" {
				t.Skipf("%s unset — set it to the weak target to run this positive check", c.env)
			}
			if u, err := url.Parse("http://" + host); err != nil || u.Host == "" {
				t.Fatalf("%s=%q is not a valid host", c.env, host)
			}
			if !binaryOnPath(c.adapter.ToolName()) {
				t.Skipf("%s not installed in this image — skipping", c.adapter.ToolName())
			}
			res, err := c.adapter.Check(domain.ScanContext{Target: c.mk(host)})
			if err != nil {
				t.Fatalf("%s: real exec+parse errored (schema drift or unreachable target): %v", c.adapter.ToolName(), err)
			}
			found := false
			for _, f := range res.Findings {
				for _, cwe := range f.CWEs {
					if cwe == c.wantCWE {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("%s: expected a %s finding from the weak target, got %d findings %v",
					c.adapter.ToolName(), c.wantCWE, len(res.Findings), res.Findings)
			}
		})
	}
}
