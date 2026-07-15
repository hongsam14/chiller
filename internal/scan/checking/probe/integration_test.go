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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "nginx/1.18.0") // a version banner httpx can report
		_, _ = w.Write([]byte(`<html><body>ok</body></html>`))
	}))
	defer srv.Close()
	host := srv.Listener.Addr().String()
	target := domain.Target{URL: srv.URL + "/", Host: host, Scheme: "http"}

	for _, a := range []Adapter{Humble(), Httpx(), Corsy()} {
		a := a
		t.Run(a.ToolName(), func(t *testing.T) {
			if !binaryOnPath(a.ToolName()) {
				t.Skipf("%s not installed in this image — skipping", a.ToolName())
			}
			// The runner bounds each exec with its own timeout (NFR-P1/NFR-12).
			_, err := a.Check(domain.ScanContext{Target: target})
			// The tool may legitimately find nothing (→ empty, nil). What must NOT
			// happen is a parse error on real output (schema drift).
			if err != nil {
				t.Errorf("%s: real exec+parse errored on a clean target (schema drift?): %v", a.ToolName(), err)
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
			if _, err := a.Check(domain.ScanContext{Target: target}); err != nil {
				t.Errorf("%s: real exec+parse errored (schema drift or unreachable target): %v", a.ToolName(), err)
			}
		})
	}
}
