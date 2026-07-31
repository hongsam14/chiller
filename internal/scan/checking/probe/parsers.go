package probe

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hongsam14/chiller/internal/domain"
)

// This file holds the real argv builders + stdout→Finding parsers for the wired
// probes (Q5=A: JSON output modes). Each parser emits Findings carrying ONLY its
// tool's owned CWE(s), so ownership → PS-ID stays unambiguous. Evidence is
// bounded to domain.MaxEvidenceItems.
//
// HONESTY (F6): every parser below is now VERIFIED against real tool output.
// The golden fixtures in testdata/ are real captures — humble/httpx/Corsy from
// the vuln-target fixture, dirhunt from an nginx autoindex, subzy from a
// takeover-fingerprint mock, testssl from rc4.badssl.com (#0d, r9). The real
// runs corrected two fabricated schemas: dirhunt (real signal is
// `index_of_processors[].crawler_url.flags:"index_of"`, not
// `results[].flags:"directory-listing"`) and subzy (real signal is the STRING
// `status:"vulnerable"`, not a boolean `vulnerable` field) — both previously
// could never emit a finding on real output. A schema drift is corrected here
// against the real golden, never by loosening the parser to hide it.

// pFinding builds a probe Finding: owned CWE, tool name as source, bounded evidence.
// Evidence is best-effort (r20, FR-E7): a probe parses an external CLI's stdout, so
// there is no HAR — only the tool name (rule attribution) and the raw line (snippet)
// are honest to record. Location / Request / Response are deliberately left zero:
// the probe did not observe them, and fabricating them would break the honesty
// contract. A downstream consumer distinguishes probe evidence by their absence.
func pFinding(cwe domain.CWE, tool, url, evidence string) domain.Finding {
	return domain.Finding{
		CWEs:       []domain.CWE{cwe},
		TargetURL:  url,
		SourceTool: tool,
		Severity:   severityFor(cwe),
		Evidence: domain.Evidence{Items: []domain.EvidenceItem{{
			Snippet:    clip(evidence),
			Rule:       tool,
			Hypothesis: hypothesisFor(cwe),
		}}},
	}
}

// hypothesisFor is the short "what this finding means" line a downstream AI uses as
// its starting hypothesis for active verification (FR-E3), keyed by owned CWE.
func hypothesisFor(cwe domain.CWE) string {
	switch cwe {
	case "CWE-1327":
		return "weak/obsolete TLS configuration"
	case "CWE-798":
		return "default or hardcoded credentials accepted"
	case "CWE-548":
		return "directory listing enabled"
	case "CWE-319":
		return "sensitive data over cleartext transport"
	case "CWE-346":
		return "permissive CORS policy"
	case "CWE-1021":
		return "missing framing protection (clickjacking)"
	case "CWE-693":
		return "missing security header / protection mechanism"
	case "CWE-200":
		return "server/version information disclosure"
	case domain.NoCanonicalCWE:
		return "dangling DNS record — subdomain takeover"
	default:
		return "passive weakness reported by " + "the owning probe"
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
// subzy v1.1.0 has NO fingerprint-path flag (verified against `subzy run --help`): it
// loads its takeover-fingerprint DB from `$HOME/subzy/fingerprints.json` and, if that
// file is absent, self-fetches it from a dead URL → parse error → tool-failed (#0e).
// So the operator mounts the DB read-only at $SUBZY_DB and the adapter runs subzy with
// HOME set so subzy reads that file instead of fetching (see subzyRunEnv).
func subzyOutArg(t domain.Target, outPath string) []string {
	return []string{"run", "--target", t.Host, "--output", outPath, "--hide_fails"}
}

// subzyDBPath is the operator-facing mount path for the DB file: the SUBZY_DB env or the
// default. It MUST be `<dir>/subzy/fingerprints.json` (the default is), because subzy
// v1.1.0 has no fingerprint flag and always reads `$HOME/subzy/fingerprints.json`.
func subzyDBPath() string {
	if p := os.Getenv("SUBZY_DB"); p != "" {
		return p
	}
	return "/opt/subzy/fingerprints.json"
}

// subzyHome is the HOME we run subzy under, and subzyReadFile is the EXACT file subzy
// then opens (`$HOME/subzy/fingerprints.json`). The guard checks subzyReadFile — not the
// literal SUBZY_DB — so the guard and subzy can never disagree: a non-conforming SUBZY_DB
// makes both point at the same (absent) derived path, failing honestly rather than
// passing the guard while subzy reads elsewhere.
func subzyHome() string     { return filepath.Dir(filepath.Dir(subzyDBPath())) }
func subzyReadFile() string { return filepath.Join(subzyHome(), "subzy", "fingerprints.json") }

// subzyRunEnv sets HOME so subzy reads the provisioned DB (subzyReadFile) instead of
// self-fetching — one fewer runtime outbound.
func subzyRunEnv() []string { return []string{"HOME=" + subzyHome()} }

// subzyDBGuard is subzy's precheck (#0e): it fails the adapter (→ tool-failed) unless the
// DB subzy WILL read is a present, non-empty fingerprint array — WITHOUT running subzy.
// Honesty must not depend on subzy's behavior: a run with zero fingerprints matches
// nothing and would report every subdomain clean (a false-clean), so we refuse to run.
func subzyDBGuard(domain.ScanContext) error {
	p := subzyReadFile()
	fi, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("subzy: fingerprint DB not provisioned at %s (run `make subzy-db` + mount it): %w", p, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("subzy: fingerprint DB path %s is a directory (missing mount / run `make subzy-db`)", p)
	}
	b, err := os.ReadFile(p) // #nosec G304 -- operator-provisioned config path, same trust boundary as configs/
	if err != nil {
		return fmt.Errorf("subzy: cannot read fingerprint DB %s: %w", p, err)
	}
	var fps []json.RawMessage
	if json.Unmarshal(b, &fps) != nil || len(fps) == 0 {
		return fmt.Errorf("subzy: fingerprint DB %s is not a non-empty JSON array (empty/malformed → would false-clean)", p)
	}
	return nil
}

// Real subzy v1.1.0 --output schema (verified against a body-matched takeover):
// a JSON array of {subdomain, status, engine, documentation, discussion}. The
// vulnerability signal is the STRING status field ("vulnerable"), NOT a boolean
// — the earlier hand-authored `vulnerable bool` never matched real output, so a
// genuinely-vulnerable subdomain was silently reported clean. `engine` is often
// empty in real output. `--hide_fails` means only flagged subdomains are written.
type subzyEntry struct {
	Subdomain string `json:"subdomain"`
	Status    string `json:"status"`
	Engine    string `json:"engine"`
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
		if !strings.EqualFold(e.Status, "vulnerable") {
			continue
		}
		ev := "subdomain takeover: " + e.Subdomain
		if e.Engine != "" {
			ev = "subdomain takeover (" + e.Engine + "): " + e.Subdomain
		}
		out = append(out, pFinding(domain.NoCanonicalCWE, "subzy", t.URL, ev))
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
			if len(out) >= domain.MaxEvidenceItems {
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

// Real dirhunt 0.9.0 --to-file schema (verified against an nginx autoindex): the
// report has an `index_of_processors` array holding exactly the URLs dirhunt
// classified as an "Index Of" (directory listing) via its ProcessIndexOfRequest
// processor. Each entry's `crawler_url.flags` carries the "index_of" flag. The
// earlier hand-authored `{results:[{url,status,flags:["directory-listing"]}]}`
// schema was fabricated — it matched nothing real, so a listed directory was
// silently reported clean. We key on the authoritative `index_of_processors`
// list (and confirm the "index_of" flag for tightness).
type dirhuntReport struct {
	IndexOf []dirhuntEntry `json:"index_of_processors"`
}

type dirhuntEntry struct {
	CrawlerURL struct {
		Flags []string        `json:"flags"`
		URL   dirhuntURLField `json:"url"`
	} `json:"crawler_url"`
}

// dirhuntURLField normalises the polymorphic `crawler_url.url` value: dirhunt
// writes `address` as a plain string in index_of entries but as a nested
// {address, domain} object elsewhere. We accept either so a schema quirk never
// crashes the parse (which would flip the probe to tool-failed).
type dirhuntURLField struct{ Address string }

func (u *dirhuntURLField) UnmarshalJSON(b []byte) error {
	var raw struct {
		Address json.RawMessage `json:"address"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	var s string
	if json.Unmarshal(raw.Address, &s) == nil {
		u.Address = s
		return nil
	}
	var nested struct {
		Address string `json:"address"`
	}
	_ = json.Unmarshal(raw.Address, &nested)
	u.Address = nested.Address
	return nil
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
	for _, e := range r.IndexOf {
		listing := false
		for _, f := range e.CrawlerURL.Flags {
			if strings.EqualFold(f, "index_of") {
				listing = true
				break
			}
		}
		if !listing {
			continue
		}
		out = append(out, pFinding("CWE-548", "dirhunt", t.URL, "directory listing exposed: "+e.CrawlerURL.URL.Address))
		if len(out) >= domain.MaxEvidenceItems {
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
		if len(out) >= domain.MaxEvidenceItems {
			break
		}
	}
	return out, nil
}
