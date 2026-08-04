package observation

import (
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/hongsam14/chiller/internal/domain"
)

// Indicative CWE assignments (one per observation item). Authoritative rows are
// pinned at Functional Design (constraint C-5); these must stay in sync with
// configs/mappings/kisa.yaml so findings' boot completeness check passes.
const (
	cwePlaintextLogin domain.CWE = "CWE-311" // PS-003
	cweRobots         domain.CWE = "CWE-651" // PS-008
	cweComment        domain.CWE = "CWE-615" // PS-009
	cweGit            domain.CWE = "CWE-527" // PS-010
	cweEnv            domain.CWE = "CWE-538" // PS-011
	cweBackup         domain.CWE = "CWE-530" // PS-012
	cweAdmin          domain.CWE = "CWE-306" // PS-013
	cweSessionURL     domain.CWE = "CWE-598" // PS-018
	cweCrossdomain    domain.CWE = "CWE-942" // PS-022
	cweCaptcha        domain.CWE = "CWE-799" // PS-023
	// Cookie-flag items — moved here from probe:humble (#0d): humble analyses
	// response headers only, never Set-Cookie attributes. The fetched response
	// already carries parsed Cookies, so these are pure in-process checks.
	cweCookieSecure   domain.CWE = "CWE-614"  // PS-015 (Secure)
	cweCookieHTTPOnly domain.CWE = "CWE-1004" // PS-016 (HttpOnly)
	cweCookieSameSite domain.CWE = "CWE-1275" // PS-017 (SameSite)
)

// hypotheses maps each owned CWE to the short "what this rule detects" line the
// downstream AI uses as its starting hypothesis for active verification (FR-E3).
var hypotheses = map[domain.CWE]string{
	cwePlaintextLogin: "login form served over http:// — credentials sent in plaintext",
	cweRobots:         "robots.txt discloses a sensitive path",
	cweComment:        "secret/token hardcoded in a comment or delivered script",
	cweGit:            ".git metadata exposed — source/history may be reconstructable",
	cweEnv:            ".env-style secret keys exposed",
	cweBackup:         "backup/archive file publicly accessible",
	cweAdmin:          "management/documentation surface reachable without authentication",
	cweSessionURL:     "session token carried in the URL",
	cweCrossdomain:    "over-permissive crossdomain.xml",
	cweCaptcha:        "login form without a CAPTCHA (brute-force exposure)",
	cweCookieSecure:   "cookie missing the Secure flag",
	cweCookieHTTPOnly: "cookie missing the HttpOnly flag",
	cweCookieSameSite: "cookie with weak/absent SameSite",
}

// ruleID is the firing rule identity (FR-E3): "observation:CWE-N" or, for the
// config-driven signatures, "observation:CWE-N/<sub-rule>" (e.g. the gitleaks rule).
func ruleID(cwe domain.CWE, sub string) string {
	if sub == "" {
		return "observation:" + string(cwe)
	}
	return "observation:" + string(cwe) + "/" + sub
}

// respHeaderAllow is the deterministic allowlist of response headers carried in
// evidence (F3): benign, reproduction-relevant, never the full header set.
// content-type is intentionally absent — it is carried in the dedicated
// ResponseContext.ContentType field, so allowlisting it here too would emit it
// twice and waste a MaxEvidenceHeaders slot.
var respHeaderAllow = map[string]bool{
	"server": true, "x-powered-by": true,
	"location": true, "www-authenticate": true, "content-length": true,
}

// responseContext builds the bounded response context for a source exchange (FR-E5).
func responseContext(src *domain.HAREntry) *domain.ResponseContext {
	if src == nil {
		return nil
	}
	rc := &domain.ResponseContext{Status: src.Status, Truncated: src.Truncated}
	for _, h := range src.Headers {
		if rc.ContentType == "" && strings.EqualFold(h.Name, "content-type") {
			rc.ContentType = clipHeader(h.Value)
		}
		if respHeaderAllow[strings.ToLower(h.Name)] && len(rc.Headers) < domain.MaxEvidenceHeaders {
			rc.Headers = append(rc.Headers, domain.Header{Name: h.Name, Value: clipHeader(h.Value)})
		}
	}
	return rc
}

func clipHeader(v string) string {
	if r := []rune(v); len(r) > domain.MaxEvidenceHeaderValueLen {
		return string(r[:domain.MaxEvidenceHeaderValueLen]) + "…"
	}
	return v
}

// findingAt builds a native observation finding with structured evidence (r20).
// targetURL is the finding identity (unchanged); src is the exchange the match
// actually came from — the page response for a body/cookie/url match, or the
// discovered JS entry for a JS-sourced match (F1). src may be nil (e.g. PS-018
// fires from the URL alone) → Request falls back to targetURL, Response is omitted.
func findingAt(cwe domain.CWE, sev domain.Severity, targetURL string, src *domain.HAREntry, item domain.EvidenceItem) domain.Finding {
	ev := domain.Evidence{Items: []domain.EvidenceItem{item}}
	if src != nil {
		ev.Request = &domain.RequestContext{Method: "GET", URL: src.URL}
		ev.Response = responseContext(src)
	} else {
		ev.Request = &domain.RequestContext{Method: "GET", URL: targetURL}
	}
	return domain.Finding{
		CWEs:       []domain.CWE{cwe},
		TargetURL:  targetURL,
		SourceTool: "observation:" + string(cwe),
		Severity:   sev,
		Evidence:   ev,
	}
}

// bodyItemAt builds a PartBody evidence item: a padded context snippet plus the
// ORIGINAL-body offset/length of the match (so a consumer can re-locate it).
func bodyItemAt(body []byte, start, end, pad int, rule, hyp string) domain.EvidenceItem {
	s, e := start-pad, end+pad
	if s < 0 {
		s = 0
	}
	if e > len(body) {
		e = len(body)
	}
	return domain.EvidenceItem{
		Snippet:    snippet(body[s:e]),
		Location:   domain.Location{Part: domain.PartBody, Offset: start, MatchLen: end - start},
		Rule:       rule,
		Hypothesis: hyp,
	}
}

// bodyMatch builds a PartBody item from re's first match in body, or ok=false when
// there is no match (so it doubles as the match test, like matchContext did).
func bodyMatch(body []byte, re *regexp.Regexp, pad int, cwe domain.CWE, sub string) (domain.EvidenceItem, bool) {
	loc := re.FindIndex(body)
	if loc == nil {
		return domain.EvidenceItem{}, false
	}
	return bodyItemAt(body, loc[0], loc[1], pad, ruleID(cwe, sub), hypotheses[cwe]), true
}

// maxEvidenceLen bounds a captured evidence snippet (FR-12/NFR-16): evidence must
// carry the ACTUAL matched data so a human or an AI can judge whether it is a real
// exposure or a false positive — but never an unbounded page dump.
const maxEvidenceLen = 200

var wsRun = regexp.MustCompile(`\s+`)

// snippet turns matched bytes into a single-line, rune-safe, length-bounded piece
// of real evidence (the actual matched content, not a description of it).
func snippet(b []byte) string {
	s := wsRun.ReplaceAllString(strings.TrimSpace(string(b)), " ")
	if r := []rune(s); len(r) > maxEvidenceLen {
		s = string(r[:maxEvidenceLen]) + "…"
	}
	return s
}

var passwordField = regexp.MustCompile(`(?i)<input[^>]+type\s*=\s*["']?password`)

func hasPasswordField(body string) bool {
	return passwordField.MatchString(body)
}

// ---- PS-003: plaintext login form over http:// ----
type plaintextLoginSig struct{}

func (plaintextLoginSig) CWE() domain.CWE { return cwePlaintextLogin }
func (plaintextLoginSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil {
		return nil, false, nil
	}
	body := string(ctx.Response.Body)
	if !hasPasswordField(body) {
		return nil, true, nil // no login form → N/A
	}
	if ctx.Response.Scheme == "http" {
		item, _ := bodyMatch(ctx.Response.Body, passwordField, 16, cwePlaintextLogin, "plaintext-login")
		return []domain.Finding{findingAt(cwePlaintextLogin, domain.SevHigh, ctx.Target.URL, ctx.Response, item)}, false, nil
	}
	return nil, false, nil
}

// ---- PS-009: sensitive info in comments/source (HTML + JS) ----
// Config-driven (r16): gitleaks-distilled secret rules (entropy-gated) + internal/
// infra disclosure, over HTML comments and delivered JS only. libPaths skips known
// minified libraries (jquery/bootstrap/…) so a library's `password:!0` never fires.
type sensitiveCommentSig struct {
	secrets  []SecretRule
	libPaths []*regexp.Regexp
	internal InternalRules
}

var htmlComment = regexp.MustCompile(`(?s)<!--(.*?)-->`)

func (sensitiveCommentSig) CWE() domain.CWE { return cweComment }
func (s sensitiveCommentSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	// Scan only HTML *comments* and delivered JS source — NOT the visible HTML body
	// (whose ordinary "Password:" prose would false-positive on every login page).
	if ctx.Response != nil {
		// FindAllSubmatchIndex (not …Submatch) so the comment body's absolute offset in
		// the response is known — the evidence item's Location.Offset must index the
		// response body, not the comment sub-slice.
		for _, idx := range htmlComment.FindAllSubmatchIndex(ctx.Response.Body, -1) {
			base := idx[2]
			if rule, data, off, mlen, ok := s.scan(ctx.Response.Body[idx[2]:idx[3]]); ok {
				item := domain.EvidenceItem{
					Snippet:    data,
					Location:   domain.Location{Part: domain.PartBody, Offset: base + off, MatchLen: mlen},
					Rule:       ruleID(cweComment, rule),
					Hypothesis: hypotheses[cweComment],
				}
				return []domain.Finding{findingAt(cweComment, domain.SevMedium, ctx.Target.URL, ctx.Response, item)}, false, nil
			}
		}
	}
	for _, js := range ctx.DiscoveredJS {
		if js == nil || s.isLibrary(js.URL) {
			continue // skip minified libraries (gitleaks global allowlist)
		}
		if rule, data, off, mlen, ok := s.scan(js.Body); ok {
			// F1: the match is in the delivered JS, not the page — attribute Request/
			// Response to that JS exchange (src=js) and pin its URL in Location.SourceURL,
			// so the consumer re-fetches the JS, not the page. TargetURL stays the page.
			item := domain.EvidenceItem{
				Snippet:    data,
				Location:   domain.Location{Part: domain.PartBody, Offset: off, MatchLen: mlen, SourceURL: js.URL},
				Rule:       ruleID(cweComment, rule),
				Hypothesis: hypotheses[cweComment],
			}
			return []domain.Finding{findingAt(cweComment, domain.SevMedium, ctx.Target.URL, js, item)}, false, nil
		}
	}
	return nil, false, nil
}

// isLibrary reports whether an asset URL matches a distilled library path (jquery/
// bootstrap/swagger-ui/node_modules/…) that gitleaks excludes from secret scanning.
func (s sensitiveCommentSig) isLibrary(url string) bool {
	for _, re := range s.libPaths {
		if re.MatchString(url) {
			return true
		}
	}
	return false
}

// scan returns (rule, evidence, offset, matchLen, ok) for the first secret or
// internal-disclosure match in b, or ok=false when clean. rule is the firing sub-
// rule id (gitleaks rule name / disclosure kind); offset/matchLen index b (r20,
// PD-A). A secret rule fires only when its regex matches AND (if it sets a
// threshold) the matched value clears the Shannon-entropy gate AND no allowlist
// regex excuses it — gitleaks' own FP model, ported.
func (s sensitiveCommentSig) scan(b []byte) (ruleName, data string, offset, matchLen int, ok bool) {
	lower := strings.ToLower(string(b)) // lower the body ONCE, not per keyword
	for _, rule := range s.secrets {
		if !hasAnyKeyword(lower, rule.Keywords) {
			continue // gitleaks keyword pre-filter (also a big perf win over 200+ regexes)
		}
		loc := rule.Re.FindSubmatchIndex(b)
		if loc == nil {
			continue
		}
		m := b[loc[0]:loc[1]] // whole match (keyword=…value context) — used for evidence
		// The secret is the capture group when the rule defines one, else the whole
		// match. gitleaks gates entropy AND its allowlist on the SECRET, not the
		// surrounding `key = …` context; applying them to the whole match instead
		// (the old bug) let an unquoted variable reference such as
		// `apiKey = Foo.apiKey` slip past generic-api-key's own `^[a-zA-Z_.-]+$`
		// allow — because that allow anchors on the value, which never matched while
		// we tested it against a string still carrying `=` and spaces.
		secret := m
		if len(loc) >= 4 && loc[2] >= 0 {
			secret = b[loc[2]:loc[3]]
		}
		if rule.MinEntropy > 0 && shannon(string(secret)) < rule.MinEntropy {
			continue // low-entropy → minified-JS literal / ordinary word, not a secret
		}
		// Allow against the secret (gitleaks' default target) AND the whole match, so
		// value-anchored allows (`^[a-zA-Z_.-]+$`) and the few match/line-anchored ones
		// (import …, LICENSE=…, --mount=type=secret) both keep excusing their cases.
		if allowed(secret, rule.Allow) || allowed(m, rule.Allow) {
			continue
		}
		start := loc[0] - 24
		if start < 0 {
			start = 0
		}
		end := loc[1] + 24
		if end > len(b) {
			end = len(b)
		}
		return "secret/" + rule.Name, snippet(b[start:end]), loc[0], loc[1] - loc[0], true
	}
	if rule, d, off, mlen, ok := s.internal.match(b); ok {
		return rule, d, off, mlen, true
	}
	return "", "", 0, 0, false
}

// allowed reports whether any allowlist regex matches the finding (a documentation
// example like AKIAIOSFODNN7EXAMPLE), excusing it.
func allowed(m []byte, allow []*regexp.Regexp) bool {
	for _, re := range allow {
		if re.Match(m) {
			return true
		}
	}
	return false
}

// hasAnyKeyword reports whether lowered contains any keyword (empty set ⇒ always,
// for keyword-less rules whose format is self-identifying).
func hasAnyKeyword(lowered string, keywords []string) bool {
	if len(keywords) == 0 {
		return true
	}
	for _, k := range keywords {
		if strings.Contains(lowered, k) {
			return true
		}
	}
	return false
}

// match returns internal-disclosure evidence (RFC1918 IP / EC2 host / non-prod
// host), or "". The RFC1918 hit gets a Go-side boundary guard replacing ZAP's PCRE
// lookaround: reject a match that is part of a longer dotted number (a version).
func (ir InternalRules) match(b []byte) (ruleName, data string, offset, matchLen int, ok bool) {
	if ir.RFC1918 != nil {
		if loc := ir.RFC1918.FindIndex(b); loc != nil {
			before := loc[0] > 0 && (b[loc[0]-1] == '.' || isDigit(b[loc[0]-1]))
			after := loc[1] < len(b) && (b[loc[1]] == '.' || isDigit(b[loc[1]]))
			if !before && !after {
				return "internal/rfc1918", snippet(b[loc[0]:loc[1]]), loc[0], loc[1] - loc[0], true
			}
		}
	}
	kinds := []string{"internal/ec2-host", "internal/nonprod-host"}
	for i, re := range []*regexp.Regexp{ir.EC2Host, ir.NonProdHosts} {
		if re == nil {
			continue
		}
		if loc := re.FindIndex(b); loc != nil {
			return kinds[i], snippet(b[loc[0]:loc[1]]), loc[0], loc[1] - loc[0], true
		}
	}
	return "", "", 0, 0, false
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// ---- PS-010: .git exposure ----
type gitExposureSig struct{}

func (gitExposureSig) CWE() domain.CWE { return cweGit }

// gitPath requires the path to be UNDER /.git (a segment boundary), so the loose
// old `Contains("/.git")` no longer trips on `/.gitignore` or `/.github/` (r10
// FP-narrow). Content anchors alone ("[core]", "ref: refs/") appear in ordinary
// ini/docs pages, so the path guard is still required.
var gitPath = regexp.MustCompile(`/\.git(/|$)`)

func (gitExposureSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil || ctx.Response.Status != 200 {
		return nil, false, nil
	}
	if !gitPath.MatchString(pathOf(ctx.Target.URL)) {
		return nil, true, nil // not a .git endpoint → N/A for this target
	}
	// config → "[core]"; HEAD → "ref: refs/". Either confirms real git metadata.
	// The body is a tiny .git artifact, so the whole (bounded) body is the evidence;
	// Location.Offset still points at the confirming marker so a consumer can anchor.
	body := ctx.Response.Body
	b := string(body)
	off := -1
	switch {
	case strings.HasPrefix(b, "ref: refs/"):
		off = 0
	case strings.Contains(b, "[core]"):
		off = strings.Index(b, "[core]")
	}
	if off >= 0 {
		item := domain.EvidenceItem{
			Snippet:    snippet(body),
			Location:   domain.Location{Part: domain.PartBody, Offset: off},
			Rule:       ruleID(cweGit, "git-exposure"),
			Hypothesis: hypotheses[cweGit],
		}
		return []domain.Finding{findingAt(cweGit, domain.SevHigh, ctx.Target.URL, ctx.Response, item)}, false, nil
	}
	return nil, false, nil
}

// ---- PS-011: .env exposure ----
type envExposureSig struct{}

// envAnchor matches a `KEY = value` line whose KEY *names* a secret. The key is
// generalised (r10) beyond the old 4-name list so prefixed/suffixed variants
// (STRIPE_SECRET_KEY, JWT_SECRET, MAIL_PASSWORD) are caught, while the original
// four names (DB_PASSWORD/APP_KEY/SECRET_KEY/AWS_SECRET_ACCESS_KEY) remain covered
// — APP_KEY in particular is not a *_KEY substring of the generic set, so it is
// listed explicitly (its loss was a silent FN regression). Group 2 captures the
// value; only a non-empty value fires (an empty `KEY=` is a commented/unset line).
// There is NO value-length floor: a short real secret (`DB_PASSWORD=p@ss!`) on a
// live .env is a real exposure. `(?im)` = case-insensitive, ^ per line.
var envAnchor = regexp.MustCompile(`(?im)^[ \t]*([A-Z0-9_]*(SECRET|PASSWORD|PASSWD|API_?KEY|APP_?KEY|ACCESS_KEY|TOKEN|PRIVATE_KEY|CREDENTIAL)[A-Z0-9_]*)[ \t]*=[ \t]*["']?([^\s"']+)`)

// envPath / envTemplatePath are the PATH guard (mirrors gitExposureSig): the
// generalised key pattern above must only judge an actual .env artifact, never an
// arbitrary 200 page that happens to start a line with `*_TOKEN=...` (that was a
// Critical false positive on any page). A template file (.env.example/.sample/
// .dist) is a public, expected artifact and holds placeholders by design → N/A.
var (
	envPath         = regexp.MustCompile(`(?i)(^|/)\.env(\.(local|prod|production|dev|development|staging|stage|test))?$`)
	envTemplatePath = regexp.MustCompile(`(?i)(^|/)\.env\.(example|sample|dist|template)$`)
)

// envPlaceholderPrefix screens out UNFILLED template tokens (`your_password`,
// `<changeme>`, `${SECRET}`, `xxxxx`, `...`) even when they appear in a real .env.
// It deliberately no longer suppresses weak-but-real values like `secret` or
// `password`: on a live, publicly-readable .env those are genuinely exposed weak
// credentials and must fire (their old blanket suppression was an FN regression).
var envPlaceholderPrefix = regexp.MustCompile(`(?i)^(your[_-]|my[_-]|<|\$\{|xxx|\.\.\.|change[_-]?me|change[_-]?it)`)

func (envExposureSig) CWE() domain.CWE { return cweEnv }
func (envExposureSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil || ctx.Response.Status != 200 {
		return nil, false, nil
	}
	p := pathOf(ctx.Target.URL)
	if envTemplatePath.MatchString(p) || !envPath.MatchString(p) {
		return nil, true, nil // public template, or not a .env file at all → N/A
	}
	body := ctx.Response.Body
	for _, loc := range envAnchor.FindAllSubmatchIndex(body, -1) {
		val := body[loc[6]:loc[7]] // group 3 = the value
		if envPlaceholderPrefix.Match(val) {
			continue // unfilled template token, not a real secret
		}
		// loc[0:1] is the matched `KEY=value` — the real exposed credential the operator
		// needs to see to judge and to rotate.
		item := bodyItemAt(body, loc[0], loc[1], 0, ruleID(cweEnv, "env-exposure"), hypotheses[cweEnv])
		return []domain.Finding{findingAt(cweEnv, domain.SevCritical, ctx.Target.URL, ctx.Response, item)}, false, nil
	}
	return nil, false, nil
}

// ---- PS-018: session id in URL ----
// sessionInURLSig (PS-018) — config-driven (r16, ZAP InformationDisclosureInUrl):
// a sensitive-named parameter (session/token/password/…) carrying a non-trivial
// value in the URL. Distilled ZAP param list. Names match by TOKEN, not substring,
// so the `token` param covers accessToken/access_token (human #3) without firing on
// benign `userType`/`passengers`. A ≥6-char value guard avoids the bare `?sid=1` FP.
type sessionInURLSig struct {
	params []string
}

func (sessionInURLSig) CWE() domain.CWE { return cweSessionURL }
func (s sessionInURLSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	u, err := url.Parse(ctx.Target.URL)
	if err != nil {
		return nil, false, nil
	}
	for _, kv := range urlParamPairs(u) {
		if len(kv.value) < 6 {
			continue // a trivial value is not a leaked session token
		}
		if s.matches(kv.name) {
			item := domain.EvidenceItem{
				Snippet:    snippet([]byte(kv.name + "=" + kv.value)),
				Location:   domain.Location{Part: domain.PartURL, Name: kv.name},
				Rule:       ruleID(cweSessionURL, "session-in-url"),
				Hypothesis: hypotheses[cweSessionURL],
			}
			// src=ctx.Response (may be nil if the fetch failed; findingAt tolerates it —
			// PS-018 fires from the URL alone). PD-B.
			return []domain.Finding{findingAt(cweSessionURL, domain.SevMedium, ctx.Target.URL, ctx.Response, item)}, false, nil
		}
	}
	return nil, false, nil
}

// matches reports whether a parameter name carries a sensitive token. It compares by
// TOKEN (name split on separators + camelCase), not substring: `access_token` /
// `accessToken` fire on the `token` param, while `userType` / `passengers` /
// `boarding_pass` do not (the old substring form false-positived on those).
func (s sessionInURLSig) matches(name string) bool {
	for _, tok := range paramTokens(name) {
		if slices.Contains(s.params, tok) {
			return true
		}
	}
	return false
}

type urlKV struct{ name, value string }

// urlParamPairs collects name=value pairs from BOTH the query string and the path's
// matrix parameters (`/page;jsessionid=…`, the classic Java URL-rewriting form),
// which url.Query() does not expose — dropping the latter was a PS-018 false-negative.
func urlParamPairs(u *url.URL) []urlKV {
	var out []urlKV
	for name, vals := range u.Query() {
		for _, v := range vals {
			out = append(out, urlKV{name, v})
		}
	}
	for _, seg := range strings.Split(u.EscapedPath(), "/") {
		parts := strings.Split(seg, ";")
		for _, mp := range parts[1:] { // parts[0] is the path element; the rest are matrix params
			if name, val, ok := strings.Cut(mp, "="); ok {
				if dv, err := url.QueryUnescape(val); err == nil {
					val = dv
				}
				out = append(out, urlKV{name, val})
			}
		}
	}
	return out
}

// paramTokens splits a parameter name into lowercase tokens on non-alphanumeric
// separators AND camelCase boundaries: `access_token`→[access token],
// `accessToken`→[access token], `userType`→[user type].
func paramTokens(name string) []string {
	var tokens []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			tokens = append(tokens, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		alnum := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !alnum {
			flush()
			continue
		}
		if r >= 'A' && r <= 'Z' && i > 0 {
			if p := runes[i-1]; p >= 'a' && p <= 'z' || p >= '0' && p <= '9' {
				flush()
			}
		}
		cur = append(cur, r)
	}
	flush()
	return tokens
}

// ---- PS-022: crossdomain.xml permissive ----
type crossdomainSig struct{}

var crossdomainWild = regexp.MustCompile(`(?i)<allow-access-from[^>]+domain\s*=\s*["']\*["']`)

func (crossdomainSig) CWE() domain.CWE { return cweCrossdomain }
func (crossdomainSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil {
		return nil, false, nil
	}
	if item, ok := bodyMatch(ctx.Response.Body, crossdomainWild, 8, cweCrossdomain, "crossdomain"); ok {
		return []domain.Finding{findingAt(cweCrossdomain, domain.SevMedium, ctx.Target.URL, ctx.Response, item)}, false, nil
	}
	return nil, false, nil
}

// ---- PS-012: backup file exposure (backup-name heuristic) ----
type backupFileSig struct{}

// backupName fires on a backup-*named* file, not on any archive (r10 FN-broaden +
// FP-narrow). Shapes: (1) a double extension `<name>.<ext>.(bak|old|save|orig)`
// e.g. index.php.bak / .env.bak; (2) a classic backup/editor suffix
// `.bak/.old/.swp/.orig/.save` or a trailing `~`; (3) an archive whose basename
// CONTAINS an unambiguous backup keyword (backup|dump|database|wwwroot) e.g.
// site-backup.zip / db_backup.tar.gz; (4) an archive whose basename IS a short
// backup token (bak|db|www|site) bounded by separators, e.g. db.sql / www.zip.
// A plain `report.zip`, `data.sql`, or `sandbox.zip` (no backup keyword as a
// bounded token) does NOT match — that was the old any-archive false positive.
// Separator-bearing names like `site-backup.zip` DO match — losing them was an FN.
var backupArchiveExt = `(zip|tar|tar\.gz|tgz|sql|7z|gz|rar)`
var backupName = regexp.MustCompile(`(?i)(` +
	`\.\w+\.(bak|old|save|orig)$` + // double extension
	`|\.(bak|old|swp|orig|save)$` + // editor/backup suffix
	`|~$` + // emacs backup
	`|(^|/)[\w.-]*(backup|dump|database|wwwroot)[\w.-]*\.` + backupArchiveExt + `$` + // long keyword anywhere in basename
	`|(^|/)([\w.-]*[-_.])?(bak|db|www|site)([-_.][\w.-]*)?\.` + backupArchiveExt + `$` + // short keyword as a bounded token
	`)`)

func (backupFileSig) CWE() domain.CWE { return cweBackup }
func (backupFileSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil || ctx.Response.Status != 200 {
		return nil, false, nil
	}
	// A real backup archive is not served as an HTML page. A soft-404 site that
	// answers /backup.zip with 200 + its homepage HTML would otherwise let the
	// engine fabricate a High from its own synthesized request — require the body
	// to NOT look like HTML before reporting (r10 FP-narrow).
	if backupName.MatchString(pathOf(ctx.Target.URL)) && !looksLikeHTML(ctx.Response) {
		item := domain.EvidenceItem{
			Snippet:    "accessible backup file: " + pathOf(ctx.Target.URL),
			Location:   domain.Location{Part: domain.PartURL},
			Rule:       ruleID(cweBackup, "backup-file"),
			Hypothesis: hypotheses[cweBackup],
		}
		return []domain.Finding{findingAt(cweBackup, domain.SevHigh, ctx.Target.URL, ctx.Response, item)}, false, nil
	}
	return nil, false, nil
}

// ---- PS-013: admin page publicly accessible (+ exposed API docs, r16) ----
type adminPageSig struct{ apiDocs APIDocRules }

// adminPath is broadened (r10 FN) to the common admin surfaces.
var adminPath = regexp.MustCompile(`(?i)/(admin|administrator|administration|manage|manager/html|wp-admin|phpmyadmin|dashboard|backend)(/|$)`)

// adminContentMarker is POSITIVE evidence that the returned body actually is an
// administrative interface, not a soft-404 homepage served for /admin. Without it
// the check concluded "reachable without authentication" from the mere ABSENCE of
// a login form — so a site that answers unknown paths with 200 + its marketing
// homepage self-generated a High from the engine's own synthesized /admin request
// (r10 FP-narrow). We require an admin-panel signal (title/heading/console
// vocabulary or a known admin product) before firing.
var adminContentMarker = regexp.MustCompile(`(?i)(admin(istrator|istration)?\s*(panel|console|dashboard|area|home|menu|login)|control\s*panel|management\s*console|phpmyadmin|wp-admin|>\s*(dashboard|users|settings|configuration|system)\s*<)`)

// loginMarker screens out pages that are actually a login/redirect or a JS-
// rendered SPA shell (r10 FP-narrow): without a browser those return 200 with no
// <input type=password> yet, which the old check misread as "reachable without
// authentication". It matches only STRONG login-page signals — an auth-required
// phrase, an SPA shell, a sign-in TITLE, or a login FORM/action — and never the
// bare word "login" anywhere in the body. The old unanchored `log[- ]?in` matched
// incidental prose like "Last login:", a `class="login-box"`, or a
// `/wp-login.php` link, which suppressed a genuinely-open admin panel (an FN).
var loginMarker = regexp.MustCompile(`(?i)(` +
	`authentication required|please authenticate|unauthori[sz]ed` +
	`|<app-root|ng-app|id=["']root["']` + // SPA shells
	`|<title[^>]*>[^<]*(sign[- ]?in|log[- ]?in)` + // sign-in page title
	`|<form[^>]*(login|signin|sign-in|auth)` + // a login form element
	`|action\s*=\s*["'][^"']*(login|signin|sign-in|auth)` + // a form posting to a login endpoint
	`)`)

func (adminPageSig) CWE() domain.CWE { return cweAdmin }
func (s adminPageSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil {
		return nil, false, nil
	}
	path := pathOf(ctx.Target.URL)
	// FR-9 (r16): an exposed API-documentation surface (nuclei-distilled path +
	// body marker) is a management-surface exposure — same owning CWE (PS-013).
	// The body marker avoids the path-only false positive a soft-404 would cause.
	if ctx.Response.Status == 200 {
		if data, off, mlen, ok := s.apiDocs.match(path, ctx.Response.Body); ok {
			item := domain.EvidenceItem{
				Snippet:    data,
				Location:   domain.Location{Part: domain.PartBody, Offset: off, MatchLen: mlen},
				Rule:       ruleID(cweAdmin, "api-docs"),
				Hypothesis: hypotheses[cweAdmin],
			}
			return []domain.Finding{findingAt(cweAdmin, domain.SevMedium, ctx.Target.URL, ctx.Response, item)}, false, nil
		}
	}
	if !adminPath.MatchString(path) {
		return nil, true, nil // not an admin/api-doc path → N/A for this target
	}
	// Public access = 200, an actual admin interface in the body, no login form,
	// and no login/SPA marker. The positive admin-content requirement is what stops
	// a soft-404 homepage from being reported as an open admin page.
	body := string(ctx.Response.Body)
	if ctx.Response.Status == 200 && adminContentMarker.MatchString(body) &&
		!hasPasswordField(body) && !loginMarker.MatchString(body) {
		item, _ := bodyMatch(ctx.Response.Body, adminContentMarker, 24, cweAdmin, "admin-open")
		return []domain.Finding{findingAt(cweAdmin, domain.SevHigh, ctx.Target.URL, ctx.Response, item)}, false, nil
	}
	return nil, false, nil
}

// ---- PS-008: robots.txt sensitive path (light anchor) ----
type robotsSensitiveSig struct{}

// robotsSensitive stays anchored to a `Disallow:` line (not a loose body match)
// and broadens the sensitive-path keyword set (r10 FN) — but each keyword must be
// a PATH SEGMENT (preceded by `/`, followed by a `/`, `.`, separator, or line end),
// NOT a bare substring. The short keywords (db/api/sql/env) as substrings matched
// ordinary paths — `/feedback` and `/cardboard` contain "db", `/rapid-delivery`
// contains "api" — turning routine robots.txt entries into false CWE-651 findings.
var robotsSensitive = regexp.MustCompile(`(?im)^\s*Disallow:\s*\S*/(admin|backup|config|secret|private|internal|database|sql|\.env|logs|db|api)($|[/.?~_-])`)

func (robotsSensitiveSig) CWE() domain.CWE { return cweRobots }
func (robotsSensitiveSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil {
		return nil, false, nil
	}
	if item, ok := bodyMatch(ctx.Response.Body, robotsSensitive, 0, cweRobots, "robots-sensitive"); ok {
		return []domain.Finding{findingAt(cweRobots, domain.SevLow, ctx.Target.URL, ctx.Response, item)}, false, nil
	}
	return nil, false, nil
}

// ---- PS-023: CAPTCHA absence on login ----
type captchaAbsenceSig struct{}

var captchaMarker = regexp.MustCompile(`(?i)(recaptcha|h-captcha|hcaptcha|g-recaptcha|cf-turnstile|captcha)`)

func (captchaAbsenceSig) CWE() domain.CWE { return cweCaptcha }
func (captchaAbsenceSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil {
		return nil, false, nil
	}
	body := string(ctx.Response.Body)
	if !hasPasswordField(body) {
		return nil, true, nil // no login form → N/A
	}
	if !captchaMarker.MatchString(body) {
		// Evidence is the login form itself (the password field) — the thing that
		// should have had a CAPTCHA. Absence has no offset of its own.
		item, _ := bodyMatch(ctx.Response.Body, passwordField, 16, cweCaptcha, "captcha-absence")
		return []domain.Finding{findingAt(cweCaptcha, domain.SevLow, ctx.Target.URL, ctx.Response, item)}, false, nil
	}
	return nil, false, nil
}

// ---- PS-015/016/017: insecure cookie flags (moved from probe:humble, #0d) ----
// The fetcher already parses Set-Cookie into ctx.Response.Cookies. Each signature
// judges ONE attribute across all cookies: any cookie missing it → a finding; no
// cookies at all → N/A (nothing to judge). One-finding-per-CWE (R7), so a single
// finding names the first offending cookie and the total count.

func offendingCookies(cs []domain.Cookie, bad func(domain.Cookie) bool) (first string, n int) {
	for _, c := range cs {
		if bad(c) {
			n++
			if first == "" {
				first = c.Name
			}
		}
	}
	return first, n
}

// cookieFinding builds the single per-CWE finding. desc states the exact defect
// (a boolean flag is "without X"; SameSite is a value, so a permissive/absent
// value is described accurately rather than as merely "without SameSite").
func cookieFinding(cwe domain.CWE, targetURL, desc, first string, n int, src *domain.HAREntry) []domain.Finding {
	ev := desc + ": " + first
	if n > 1 {
		ev = ev + " (+" + itoa(n-1) + " more)"
	}
	item := domain.EvidenceItem{
		Snippet:    ev,
		Location:   domain.Location{Part: domain.PartCookie, Name: first},
		Rule:       ruleID(cwe, "cookie-flags"),
		Hypothesis: hypotheses[cwe],
	}
	return []domain.Finding{findingAt(cwe, domain.SevMedium, targetURL, src, item)}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

type cookieSecureSig struct{}

func (cookieSecureSig) CWE() domain.CWE { return cweCookieSecure }
func (cookieSecureSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil || len(ctx.Response.Cookies) == 0 {
		return nil, true, nil // no cookies → not applicable
	}
	if first, n := offendingCookies(ctx.Response.Cookies, func(c domain.Cookie) bool { return !c.Secure }); n > 0 {
		return cookieFinding(cweCookieSecure, ctx.Target.URL, "cookie without Secure", first, n, ctx.Response), false, nil
	}
	return nil, false, nil
}

type cookieHTTPOnlySig struct{}

func (cookieHTTPOnlySig) CWE() domain.CWE { return cweCookieHTTPOnly }
func (cookieHTTPOnlySig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil || len(ctx.Response.Cookies) == 0 {
		return nil, true, nil
	}
	if first, n := offendingCookies(ctx.Response.Cookies, func(c domain.Cookie) bool { return !c.HttpOnly }); n > 0 {
		return cookieFinding(cweCookieHTTPOnly, ctx.Target.URL, "cookie without HttpOnly", first, n, ctx.Response), false, nil
	}
	return nil, false, nil
}

type cookieSameSiteSig struct{}

func (cookieSameSiteSig) CWE() domain.CWE { return cweCookieSameSite }
func (cookieSameSiteSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil || len(ctx.Response.Cookies) == 0 {
		return nil, true, nil
	}
	// SameSite absent (empty) or the permissive "None" both leave CSRF exposure.
	if first, n := offendingCookies(ctx.Response.Cookies, func(c domain.Cookie) bool {
		return c.SameSite == "" || strings.EqualFold(c.SameSite, "None")
	}); n > 0 {
		return cookieFinding(cweCookieSameSite, ctx.Target.URL, "cookie with weak SameSite (missing or None)", first, n, ctx.Response), false, nil
	}
	return nil, false, nil
}

// pathOf returns the URL path only (no query), so suffix anchors like `.bak$`
// are not defeated by a trailing `?query`. Best-effort; safe on parse error.
func pathOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Path != "" {
		return u.Path
	}
	return raw
}

// looksLikeHTML reports whether a response body is an HTML page — used to reject
// soft-404s (a 200 + homepage HTML returned for a path that does not really
// exist). A genuine backup archive (.zip/.sql/.bak) is never served as text/html.
// It trusts an explicit Content-Type when present, and otherwise sniffs the body
// for a leading HTML marker.
func looksLikeHTML(r *domain.HAREntry) bool {
	if r == nil {
		return false
	}
	for _, h := range r.Headers {
		if strings.EqualFold(h.Name, "Content-Type") {
			ct := strings.ToLower(h.Value)
			return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml")
		}
	}
	b := strings.TrimLeft(strings.ToLower(string(r.Body)), " \t\r\n")
	return strings.HasPrefix(b, "<!doctype html") || strings.HasPrefix(b, "<html") || strings.HasPrefix(b, "<head") || strings.HasPrefix(b, "<!doctype")
}
