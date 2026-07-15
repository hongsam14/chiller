package probe

import (
	"bufio"
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/hongsam14/chiller/internal/domain"
)

// This file holds the real argv builders + stdout→Finding parsers for the wired
// probes (Q5=A: JSON output modes). Each parser emits Findings carrying ONLY its
// tool's owned CWE(s), so ownership → PS-ID stays unambiguous. Evidence is
// bounded to domain.MaxEvidenceSnippets.
//
// HONESTY (F6): these JSON schemas are authored against each tool's documented
// output but are UNVERIFIED against a real run until the in-image integration
// suite executes the pinned binary (Build & Test). Golden fixtures in testdata/
// pin the assumed schema; a schema drift is corrected there, not by loosening
// the parser.

// pFinding builds a probe Finding: owned CWE, tool name as source, bounded evidence.
func pFinding(cwe domain.CWE, tool, url, evidence string) domain.Finding {
	return domain.Finding{
		CWEs:       []domain.CWE{cwe},
		TargetURL:  url,
		SourceTool: tool,
		Severity:   severityFor(cwe),
		Evidence:   domain.Evidence{Snippets: []string{clip(evidence)}},
	}
}

// severityFor assigns a sensible severity per owned CWE; the findings pipeline
// may re-normalise via configs/severity.yaml (R4 just requires Valid).
func severityFor(cwe domain.CWE) domain.Severity {
	switch cwe {
	case "CWE-1327", "CWE-798", "CWE-548": // weak TLS, default creds, dir listing
		return domain.SevHigh
	case "CWE-319", "CWE-346": // cleartext transport, permissive CORS
		return domain.SevMedium
	case domain.NoCanonicalCWE: // subdomain takeover (attacker axis)
		return domain.SevHigh
	default: // cookie flags, version disclosure, clickjacking
		return domain.SevLow
	}
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	const max = 240
	if len(s) > max {
		return s[:max]
	}
	return s
}

// ---- httpx (URL, PS-007/020 · CWE-693 version disclosure) ----
// httpx -json emits one JSON object per line (JSONL).
func httpxArgv(t domain.Target) []string {
	return []string{"-json", "-silent", "-no-color", "-u", t.URL}
}

type httpxLine struct {
	URL       string   `json:"url"`
	Webserver string   `json:"webserver"`
	Tech      []string `json:"tech"`
}

func httpxParse(t domain.Target, stdout []byte) ([]domain.Finding, error) {
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var h httpxLine
		if err := json.Unmarshal(line, &h); err != nil {
			return nil, err // malformed tool output → tool-failed, not silent clean
		}
		// A server/framework banner that carries a version string is the disclosure.
		if hasVersion(h.Webserver) {
			return []domain.Finding{pFinding("CWE-693", "httpx", t.URL, "server banner discloses version: "+h.Webserver)}, nil
		}
		for _, tech := range h.Tech {
			if hasVersion(tech) {
				return []domain.Finding{pFinding("CWE-693", "httpx", t.URL, "technology banner discloses version: "+tech)}, nil
			}
		}
	}
	return nil, sc.Err()
}

// hasVersion reports whether s carries a dotted numeric version (e.g. nginx/1.18.0).
func hasVersion(s string) bool {
	digits, dot := false, false
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			digits = true
		}
		if s[i] == '.' && digits {
			dot = true
		}
	}
	return digits && dot
}

// ---- subzy (Host, PS-025 · NoCanonicalCWE subdomain takeover) ----
// subzy writes JSON to --output <FILE> (not stdout); stdout carries banner text.
func subzyOutArg(t domain.Target, outPath string) []string {
	return []string{"run", "--target", t.Host, "--output", outPath, "--hide_fails"}
}

type subzyEntry struct {
	Subdomain  string `json:"subdomain"`
	Engine     string `json:"engine"`
	Vulnerable bool   `json:"vulnerable"`
}

func subzyParse(t domain.Target, stdout []byte) ([]domain.Finding, error) {
	stdout = bytes.TrimSpace(stdout)
	if len(stdout) == 0 {
		return nil, nil
	}
	var entries []subzyEntry
	if err := json.Unmarshal(stdout, &entries); err != nil {
		return nil, err
	}
	var out []domain.Finding
	for _, e := range entries {
		if e.Vulnerable {
			out = append(out, pFinding(domain.NoCanonicalCWE, "subzy", t.URL,
				"subdomain takeover ("+e.Engine+"): "+e.Subdomain))
		}
	}
	return out, nil
}

// ---- testssl.sh (Host, PS-002 · CWE-1327 weak TLS) ----
// --jsonfile writes the JSON array to a FILE (stdout carries the human report).
// --openssl points at the bundled static openssl (IQ3=B).
func testsslOutArg(t domain.Target, outPath string) []string {
	return []string{
		"--jsonfile", outPath, "--quiet", "--fast", "--protocols", "--standard",
		"--openssl=/opt/testssl/bin/openssl.Linux.x86_64",
		t.Host + ":443",
	}
}

type testsslEntry struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Finding  string `json:"finding"`
}

func testsslParse(t domain.Target, stdout []byte) ([]domain.Finding, error) {
	stdout = bytes.TrimSpace(stdout)
	if len(stdout) == 0 {
		return nil, nil
	}
	var entries []testsslEntry
	if err := json.Unmarshal(stdout, &entries); err != nil {
		return nil, err
	}
	var out []domain.Finding
	for _, e := range entries {
		switch strings.ToUpper(e.Severity) {
		case "HIGH", "CRITICAL":
			out = append(out, pFinding("CWE-1327", "testssl.sh", t.URL,
				e.ID+": "+e.Finding))
			if len(out) >= domain.MaxEvidenceSnippets {
				return out, nil
			}
		}
	}
	return out, nil
}

// ---- humble (URL, PS-001 HSTS + PS-024 clickjacking) ----
// humble (rfc-st/humble) analyses RESPONSE SECURITY HEADERS only — it does NOT
// inspect cookies, so PS-015/016/017 (cookie Secure/HttpOnly/SameSite) are NOT
// humble's to judge; those moved to the observation engine (Set-Cookie sigs).
// humble owns exactly CWE-319 (HSTS) + CWE-1021 (clickjacking).
//
// Real schema (verified against Juice Shop): a JSON object whose keys are
// numbered section titles ("[1. Enabled HTTP Security Headers]", "[2. Missing
// HTTP Security Headers]", ...). Each header section is an array of {Header,
// Details, References}. We match sections by substring so a shift in the section
// number does not break parsing.
func humbleOutArg(t domain.Target, outPath string) []string {
	// humble splits output into -op <dir> + -of <filename>; a full path in -of
	// alone hits a permission error. Feeding dir+base of the runner's temp path
	// makes humble write exactly to outPath.
	return []string{"-u", t.URL, "-o", "json", "-op", filepath.Dir(outPath), "-of", filepath.Base(outPath)}
}

type humbleHeaderItem struct {
	Header string `json:"Header"`
}

// humbleSection returns the header names in the first section whose title
// contains want (e.g. "Missing HTTP Security Headers"). Header names may carry a
// leading "(*) " experimental marker, which is trimmed. A matched section whose
// body fails to unmarshal is surfaced as an error (F6 honesty invariant) so the
// probe is marked tool-failed rather than silently producing no findings.
func humbleSection(doc map[string]json.RawMessage, want string) ([]string, error) {
	for title, raw := range doc {
		if !strings.Contains(title, want) {
			continue
		}
		var items []humbleHeaderItem
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		names := make([]string, 0, len(items))
		for _, it := range items {
			names = append(names, strings.TrimSpace(strings.TrimPrefix(it.Header, "(*)")))
		}
		return names, nil
	}
	return nil, nil
}

func humbleParse(t domain.Target, stdout []byte) ([]domain.Finding, error) {
	stdout = bytes.TrimSpace(stdout)
	if len(stdout) == 0 {
		return nil, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(stdout, &doc); err != nil {
		return nil, err // malformed tool output → tool-failed, not silent clean
	}
	missing, err := humbleSection(doc, "Missing HTTP Security Headers")
	if err != nil {
		return nil, err // malformed section body → tool-failed, not silent clean
	}
	has := func(names []string, h string) bool {
		for _, n := range names {
			if strings.EqualFold(n, h) {
				return true
			}
		}
		return false
	}
	var out []domain.Finding
	// PS-001 (CWE-319): HSTS not set → cleartext-transport downgrade risk.
	if has(missing, "Strict-Transport-Security") {
		out = append(out, pFinding("CWE-319", "humble", t.URL, "Strict-Transport-Security header missing (no HSTS)"))
	}
	// PS-024 (CWE-1021): clickjacking unprotected only when BOTH X-Frame-Options
	// and a Content-Security-Policy (which could carry frame-ancestors) are
	// absent. A target that sets either (e.g. Juice Shop's X-Frame-Options) is
	// clean — no false positive.
	if has(missing, "X-Frame-Options") && has(missing, "Content-Security-Policy") {
		out = append(out, pFinding("CWE-1021", "humble", t.URL, "no clickjacking protection (X-Frame-Options and CSP both absent)"))
	}
	return out, nil
}

// ---- dirhunt (Host, PS-004 · CWE-548 directory listing) ----
// dirhunt writes its JSON report to --to-file <FILE>; stdout is a live console.
// Bounded to the target itself: without --exclude-sources it queries Google/
// CommonCrawl/etc. and effectively hangs (the first in-image run timed out at
// 180s). We keep it a shallow, self-contained passive crawl.
func dirhuntOutArg(t domain.Target, outPath string) []string {
	return []string{
		t.Scheme + "://" + t.Host,
		"--to-file", outPath,
		"--exclude-sources", "robots,virustotal,google,commoncrawl,crtsh,certificatessl,wayback",
		"--not-follow-subdomains",
		"--max-depth", "1",
		"--timeout", "15",
	}
}

type dirhuntReport struct {
	Results []dirhuntResult `json:"results"`
}

type dirhuntResult struct {
	URL    string   `json:"url"`
	Status int      `json:"status"`
	Flags  []string `json:"flags"`
}

func dirhuntParse(t domain.Target, stdout []byte) ([]domain.Finding, error) {
	stdout = bytes.TrimSpace(stdout)
	if len(stdout) == 0 {
		return nil, nil
	}
	var r dirhuntReport
	if err := json.Unmarshal(stdout, &r); err != nil {
		return nil, err
	}
	var out []domain.Finding
	for _, res := range r.Results {
		for _, f := range res.Flags {
			if strings.Contains(strings.ToLower(f), "directory-listing") || strings.Contains(strings.ToLower(f), "index-of") {
				out = append(out, pFinding("CWE-548", "dirhunt", t.URL, "directory listing exposed: "+res.URL))
				break
			}
		}
		if len(out) >= domain.MaxEvidenceSnippets {
			break
		}
	}
	return out, nil
}

// ---- Corsy (URL, PS-021 · CWE-346 permissive CORS) ----
// Corsy writes JSON to -o <FILE> (stdout carries the banner); -q suppresses tips.
func corsyOutArg(t domain.Target, outPath string) []string {
	return []string{"-u", t.URL, "-o", outPath, "-q"}
}

// Real Corsy schema (verified against Juice Shop): a JSON object keyed by the
// tested URL, each value {class, description, severity, exploitation, "acao
// header", "acac header"}. Any entry present IS a CORS misconfiguration Corsy
// flagged (it only writes entries it found). We map every entry to CWE-346 and
// carry the misconfig class + the reflected ACAO/ACAC values as evidence.
type corsyEntry struct {
	Class    string `json:"class"`
	Severity string `json:"severity"`
	ACAO     string `json:"acao header"`
	ACAC     string `json:"acac header"`
}

func corsyParse(t domain.Target, stdout []byte) ([]domain.Finding, error) {
	stdout = bytes.TrimSpace(stdout)
	if len(stdout) == 0 {
		return nil, nil
	}
	var doc map[string]corsyEntry
	if err := json.Unmarshal(stdout, &doc); err != nil {
		return nil, err
	}
	var out []domain.Finding
	for url, e := range doc {
		out = append(out, pFinding("CWE-346", "corsy", t.URL,
			"CORS misconfiguration ["+e.Class+"] at "+url+" (ACAO="+e.ACAO+", ACAC="+e.ACAC+")"))
		if len(out) >= domain.MaxEvidenceSnippets {
			break
		}
	}
	return out, nil
}
