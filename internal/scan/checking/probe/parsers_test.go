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
	// Real humble output captured against OWASP Juice Shop (#0d): HSTS is missing
	// (→ CWE-319) but X-Frame-Options IS set, so clickjacking (CWE-1021) is clean —
	// exactly one finding, and NO false positive for the present header. humble
	// owns only header CWEs now; cookies moved to the observation engine.
	fs, err := humbleParse(probeTarget, golden(t, "humble.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].CWEs[0] != "CWE-319" {
		t.Fatalf("want exactly 1 CWE-319 (HSTS) finding from real Juice Shop output, got %v", cwesOf(fs))
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
