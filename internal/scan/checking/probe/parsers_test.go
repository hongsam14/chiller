package probe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

var probeTarget = domain.Target{URL: "https://a.test/", Host: "a.test", Scheme: "https"}

func golden(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return b
}

// cwesOf collects the single CWE from each finding (probe findings own one CWE).
func cwesOf(fs []domain.Finding) []domain.CWE {
	out := make([]domain.CWE, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.CWEs...)
	}
	return out
}

func assertValid(t *testing.T, fs []domain.Finding, wantTool string) {
	t.Helper()
	for _, f := range fs {
		if !f.Severity.Valid() {
			t.Errorf("%s: finding has invalid severity %v", wantTool, f.Severity)
		}
		if f.SourceTool != wantTool {
			t.Errorf("source tool = %q, want %q", f.SourceTool, wantTool)
		}
		if len(f.CWEs) != 1 {
			t.Errorf("%s: probe finding must own exactly 1 CWE, got %v", wantTool, f.CWEs)
		}
		if len(f.Evidence.Snippets) > domain.MaxEvidenceSnippets {
			t.Errorf("%s: evidence snippets %d exceed bound %d", wantTool, len(f.Evidence.Snippets), domain.MaxEvidenceSnippets)
		}
	}
}

func TestHttpxParse(t *testing.T) {
	fs, err := httpxParse(probeTarget, golden(t, "httpx.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].CWEs[0] != "CWE-693" {
		t.Fatalf("want 1 CWE-693 finding, got %v", cwesOf(fs))
	}
	assertValid(t, fs, "httpx")

	// A banner with no version → clean (no false positive).
	clean, err := httpxParse(probeTarget, golden(t, "httpx_clean.json"))
	if err != nil || len(clean) != 0 {
		t.Fatalf("no-version banner must be clean, got %v err %v", cwesOf(clean), err)
	}
}

// r18 (#0e): subzy v1.1.0 has no fingerprint-path flag — it reads
// `$HOME/subzy/fingerprints.json`. The adapter sets HOME so that resolves to the
// mounted DB (SUBZY_DB, default /opt/subzy/fingerprints.json → HOME=/opt).
func TestSubzyRunEnv_HomeFromDB(t *testing.T) {
	t.Setenv("SUBZY_DB", "") // default
	if got := subzyRunEnv(); len(got) != 1 || got[0] != "HOME=/opt" {
		t.Errorf("default subzy env = %v, want [HOME=/opt] (for /opt/subzy/fingerprints.json)", got)
	}
	t.Setenv("SUBZY_DB", "/mnt/data/subzy/fingerprints.json")
	if got := subzyRunEnv(); len(got) != 1 || got[0] != "HOME=/mnt/data" {
		t.Errorf("override subzy env = %v, want [HOME=/mnt/data]", got)
	}
	// subzyOutArg must NOT carry a fingerprint flag (v1.1.0 has none).
	for _, a := range subzyOutArg(probeTarget, "/tmp/o.json") {
		if a == "--vuln_list" {
			t.Error("subzy v1.1.0 has no --vuln_list flag; it must not be passed")
		}
	}
}

// r18 (#0e), review P1/P3/P4: the guard checks the EXACT file subzy reads
// ($HOME/subzy/fingerprints.json, derived from SUBZY_DB) and fails the adapter
// (→ tool-failed) on absent / directory / empty-array / malformed — WITHOUT running
// subzy — so a fingerprint-less run can never masquerade as a clean scan (false-clean).
func TestSubzyDBGuard(t *testing.T) {
	home := t.TempDir()
	db := filepath.Join(home, "subzy", "fingerprints.json") // conforming: <HOME>/subzy/fingerprints.json
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUBZY_DB", db)

	// (a) missing → fail.
	if err := subzyDBGuard(domain.ScanContext{}); err == nil {
		t.Error("missing DB must fail the guard (→ tool-failed)")
	}
	// (b) empty array [] → fail (review F4: semantically fingerprint-less).
	if err := os.WriteFile(db, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := subzyDBGuard(domain.ScanContext{}); err == nil {
		t.Error("'[]' has no fingerprints → must fail (never false-clean)")
	}
	// (c) malformed JSON → fail.
	if err := os.WriteFile(db, []byte("<html>404</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := subzyDBGuard(domain.ScanContext{}); err == nil {
		t.Error("malformed DB must fail the guard")
	}
	// (d) a valid, non-empty fingerprint array → passes.
	if err := os.WriteFile(db, []byte(`[{"fingerprint":"x","service":"y","status":"Vulnerable"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := subzyDBGuard(domain.ScanContext{}); err != nil {
		t.Errorf("a provisioned non-empty fingerprint DB must pass the guard, got %v", err)
	}
	// (e) a directory where the file should be → fail (review F3: Docker auto-creates a dir).
	dir := t.TempDir()
	dbdir := filepath.Join(dir, "subzy", "fingerprints.json")
	if err := os.MkdirAll(dbdir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUBZY_DB", dbdir)
	if err := subzyDBGuard(domain.ScanContext{}); err == nil {
		t.Error("a directory at the DB path must fail the guard (missing mount)")
	}
}

func TestSubzyParse(t *testing.T) {
	fs, err := subzyParse(probeTarget, golden(t, "subzy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].CWEs[0] != domain.NoCanonicalCWE {
		t.Fatalf("want 1 no-canonical (takeover) finding, got %v", cwesOf(fs))
	}
	assertValid(t, fs, "subzy")
}

func TestTestsslParse(t *testing.T) {
	fs, err := testsslParse(probeTarget, golden(t, "testssl.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Only the HIGH entry becomes a finding; OK/MEDIUM are not weak-TLS HIGH.
	if len(fs) != 1 || fs[0].CWEs[0] != "CWE-1327" {
		t.Fatalf("want 1 CWE-1327 finding from the HIGH entry, got %v", cwesOf(fs))
	}
	assertValid(t, fs, "testssl.sh")
}

func TestHumbleParse(t *testing.T) {
	// Real humble output captured against the vuln-target fixture (#0d, r9): the
	// fixture sets NO security headers, so both HSTS (→ CWE-319) and clickjacking
	// protection (X-Frame-Options AND CSP both absent → CWE-1021) are missing →
	// exactly two findings. humble owns only header CWEs now; cookies moved to the
	// observation engine.
	fs, err := humbleParse(probeTarget, golden(t, "humble.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[domain.CWE]bool{}
	for _, c := range cwesOf(fs) {
		got[c] = true
	}
	if len(fs) != 2 || !got["CWE-319"] || !got["CWE-1021"] {
		t.Fatalf("want CWE-319 (HSTS) + CWE-1021 (clickjacking) from real fixture output, got %v", cwesOf(fs))
	}
	assertValid(t, fs, "humble")
}

// When BOTH X-Frame-Options and CSP are missing, humble reports clickjacking.
func TestHumbleParse_Clickjacking(t *testing.T) {
	in := []byte(`{"[2. Missing HTTP Security Headers]":[{"Header":"Strict-Transport-Security"},{"Header":"X-Frame-Options"},{"Header":"Content-Security-Policy"}]}`)
	fs, err := humbleParse(probeTarget, in)
	if err != nil {
		t.Fatal(err)
	}
	got := map[domain.CWE]bool{}
	for _, c := range cwesOf(fs) {
		got[c] = true
	}
	if !got["CWE-319"] || !got["CWE-1021"] {
		t.Errorf("want CWE-319 + CWE-1021 when HSTS/XFO/CSP all missing, got %v", cwesOf(fs))
	}
}

// The outer humble object parses, but the matched section body is schema-drifted
// (an object where an array of header items is expected). This must surface as an
// error (→ tool-failed), not a silent clean parse. Regression for the case where
// humbleSection swallowed the section-body unmarshal error and returned nil.
func TestHumbleParse_MalformedSectionBody_IsError(t *testing.T) {
	in := []byte(`{"[2. Missing HTTP Security Headers]":{"Header":"Strict-Transport-Security"}}`)
	if _, err := humbleParse(probeTarget, in); err == nil {
		t.Error("schema-drifted humble section body must error (→ tool-failed), got nil")
	}
}

func TestDirhuntParse(t *testing.T) {
	fs, err := dirhuntParse(probeTarget, golden(t, "dirhunt.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].CWEs[0] != "CWE-548" {
		t.Fatalf("want 1 CWE-548 (dir listing) finding, got %v", cwesOf(fs))
	}
	assertValid(t, fs, "dirhunt")
}

func TestCorsyParse(t *testing.T) {
	fs, err := corsyParse(probeTarget, golden(t, "corsy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].CWEs[0] != "CWE-346" {
		t.Fatalf("want 1 CWE-346 (CORS) finding, got %v", cwesOf(fs))
	}
	assertValid(t, fs, "corsy")
}

// Malformed tool output must be an error (→ tool-failed), never silent clean.
func TestParsers_MalformedJSON_IsError(t *testing.T) {
	bad := []byte("{not json")
	for name, p := range map[string]Parser{
		"subzy": subzyParse, "testssl": testsslParse, "humble": humbleParse,
		"dirhunt": dirhuntParse, "corsy": corsyParse,
	} {
		if _, err := p(probeTarget, bad); err == nil {
			t.Errorf("%s: malformed JSON must error (→ tool-failed), got nil", name)
		}
	}
}

// An empty stdout (tool ran, found nothing to report) is a clean result, not an
// error — the item lands Checked.
func TestParsers_EmptyStdout_IsClean(t *testing.T) {
	for name, p := range map[string]Parser{
		"subzy": subzyParse, "testssl": testsslParse, "humble": humbleParse,
		"dirhunt": dirhuntParse, "corsy": corsyParse, "httpx": httpxParse,
	} {
		fs, err := p(probeTarget, []byte("  "))
		if err != nil || len(fs) != 0 {
			t.Errorf("%s: empty stdout must be clean (nil,nil), got %v err %v", name, cwesOf(fs), err)
		}
	}
}
