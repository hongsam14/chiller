package observation

import (
	"os"
	"path/filepath"
	"testing"
)

// LoadRules is fail-closed (SR: a scanner that silently loaded zero secret rules
// would report false-clean). These tests pin that every missing file, missing
// required field, and un-compilable regex aborts startup rather than degrading.

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// validSet writes a minimal, well-formed config set into dir; individual tests
// then overwrite one file to inject a specific fault.
func validSet(t *testing.T, dir string) {
	writeFile(t, dir, "secrets.yaml", "rules:\n  - name: r\n    regex: 'abc'\n    keywords: [abc]\n")
	writeFile(t, dir, "url-sensitive.yaml", "params: [token]\n")
	writeFile(t, dir, "internal-disclosure.yaml", "rfc1918: '10\\.0\\.0\\.1'\nec2Host: 'x'\nnonProdHosts: 'y'\n")
	writeFile(t, dir, "api-docs.yaml", "paths: [/swagger.json]\nbodyMarkers: [openapi]\n")
	writeFile(t, dir, "exposures.yaml", "paths: [/actuator/env]\nmarkers: [activeProfiles]\n")
	writeFile(t, dir, "error-disclosure.yaml", "stackTrace: ['Traceback \\(most recent call last\\)']\nerrorInfo: ['Whitelabel Error Page']\n")
}

func TestLoadRules_FailClosed(t *testing.T) {
	t.Run("missing dir", func(t *testing.T) {
		if _, err := LoadRules(filepath.Join(t.TempDir(), "nope")); err == nil {
			t.Fatal("want error for missing directory")
		}
	})

	t.Run("missing one file", func(t *testing.T) {
		dir := t.TempDir()
		validSet(t, dir)
		if err := os.Remove(filepath.Join(dir, "api-docs.yaml")); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadRules(dir); err == nil {
			t.Fatal("want error when a signature file is absent")
		}
	})

	t.Run("uncompilable secret regex", func(t *testing.T) {
		dir := t.TempDir()
		validSet(t, dir)
		writeFile(t, dir, "secrets.yaml", "rules:\n  - name: bad\n    regex: '('\n    keywords: [x]\n")
		if _, err := LoadRules(dir); err == nil {
			t.Fatal("want error for an un-compilable secret regex")
		}
	})

	t.Run("missing internal field", func(t *testing.T) {
		dir := t.TempDir()
		validSet(t, dir)
		writeFile(t, dir, "internal-disclosure.yaml", "ec2Host: 'x'\nnonProdHosts: 'y'\n") // no rfc1918
		if _, err := LoadRules(dir); err == nil {
			t.Fatal("want error when a required internal-disclosure field is empty")
		}
	})

	t.Run("valid set loads", func(t *testing.T) {
		dir := t.TempDir()
		validSet(t, dir)
		r, err := LoadRules(dir)
		if err != nil {
			t.Fatalf("valid set must load: %v", err)
		}
		if len(r.Secrets) == 0 || len(r.URLParams) == 0 || r.Internal.RFC1918 == nil || len(r.APIDocs.Paths) == 0 {
			t.Fatalf("valid set produced empty rules: %+v", r)
		}
	})
}

// TestLoadRules_ShippedConfigNonEmpty guards the real config that ships: the boot
// loader must yield a substantial rule set, never a silently-empty one.
func TestLoadRules_ShippedConfigNonEmpty(t *testing.T) {
	r := testRules(t)
	if len(r.Secrets) < 100 {
		t.Errorf("shipped secrets.yaml has %d rules, want the full distilled set (>=100)", len(r.Secrets))
	}
	if len(r.LibraryPaths) == 0 {
		t.Error("shipped secrets.yaml must carry the library allowlist (jQuery-FP fix)")
	}
	if len(r.URLParams) == 0 || r.Internal.RFC1918 == nil || len(r.APIDocs.Paths) == 0 {
		t.Error("shipped config missing url-sensitive / internal / api-docs rules")
	}
}

// Property: the RFC1918 boundary guard (Go replacement for ZAP's PCRE lookaround)
// fires on a delimited private IP and stays silent when the same IP is embedded in
// a longer dotted number — for every octet combination tried.
func TestProperty_RFC1918BoundaryGuard(t *testing.T) {
	ir := testRules(t).Internal
	ips := []string{"10.0.0.1", "10.255.13.7", "172.16.0.5", "172.31.255.254", "192.168.1.1"}
	for _, ip := range ips {
		// delimited by non-digit/non-dot on both sides → must fire
		if _, _, _, _, ok := ir.match([]byte("host=" + ip + " end")); !ok {
			t.Errorf("delimited private IP %q should be detected", ip)
		}
		// embedded in a longer dotted number (leading and trailing digit runs) → must not
		if _, got, _, _, ok := ir.match([]byte("build 3." + ip + ".9")); ok {
			t.Errorf("IP embedded in a version number (3.%s.9) must not fire, got %q", ip, got)
		}
	}
	// a public IP is never RFC1918
	if _, got, _, _, ok := ir.match([]byte("peer 8.8.8.8 ok")); ok {
		t.Errorf("public IP must not match RFC1918, got %q", got)
	}
}
