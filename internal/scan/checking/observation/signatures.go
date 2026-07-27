package observation

import (
	"net/url"
	"regexp"
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

// finding is a small helper to build a native observation finding.
func finding(cwe domain.CWE, url string, sev domain.Severity, evidence string) domain.Finding {
	return domain.Finding{
		CWEs:       []domain.CWE{cwe},
		TargetURL:  url,
		SourceTool: "observation:" + string(cwe),
		Severity:   sev,
		Evidence:   domain.Evidence{Snippets: []string{evidence}},
	}
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
		return []domain.Finding{finding(cwePlaintextLogin, ctx.Target.URL, domain.SevHigh, "password field served over http://")}, false, nil
	}
	return nil, false, nil
}

// ---- PS-009: sensitive info in comments/source (HTML + JS) ----
type sensitiveCommentSig struct{}

var (
	secretAnchor = regexp.MustCompile(`(?i)(password\s*[=:]|api[_-]?key|secret[_-]?key|aws_secret|BEGIN [A-Z ]*PRIVATE KEY|todo:.*(fixme|hack)|internal[_-]?ip)`)
	htmlComment  = regexp.MustCompile(`(?s)<!--(.*?)-->`)
)

func (sensitiveCommentSig) CWE() domain.CWE { return cweComment }
func (sensitiveCommentSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	// Scan only HTML *comments* and delivered JS source — NOT the visible HTML
	// body, whose ordinary text (e.g. a "Password:" label) would otherwise trip
	// secretAnchor on every login page (false positive).
	if ctx.Response != nil {
		for _, c := range htmlComment.FindAllSubmatch(ctx.Response.Body, -1) {
			if secretAnchor.Match(c[1]) {
				return []domain.Finding{finding(cweComment, ctx.Target.URL, domain.SevMedium, "sensitive token in HTML comment")}, false, nil
			}
		}
	}
	for _, js := range ctx.DiscoveredJS {
		if js != nil && secretAnchor.Match(js.Body) {
			return []domain.Finding{finding(cweComment, ctx.Target.URL, domain.SevMedium, "sensitive token in client-delivered JS source")}, false, nil
		}
	}
	return nil, false, nil
}

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
	b := string(ctx.Response.Body)
	if strings.HasPrefix(b, "ref: refs/") || strings.Contains(b, "[core]") {
		return []domain.Finding{finding(cweGit, ctx.Target.URL, domain.SevHigh, ".git metadata exposed")}, false, nil
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
	for _, m := range envAnchor.FindAllSubmatch(ctx.Response.Body, -1) {
		val := m[3]
		if envPlaceholderPrefix.Match(val) {
			continue // unfilled template token, not a real secret
		}
		return []domain.Finding{finding(cweEnv, ctx.Target.URL, domain.SevCritical, ".env with secret keys exposed")}, false, nil
	}
	return nil, false, nil
}

// ---- PS-018: session id in URL ----
type sessionInURLSig struct{}

var sessionInURL = regexp.MustCompile(`(?i)[?&/;](jsessionid|phpsessid|sid|sessionid|session_id)=`)

func (sessionInURLSig) CWE() domain.CWE { return cweSessionURL }
func (sessionInURLSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if sessionInURL.MatchString(ctx.Target.URL) {
		return []domain.Finding{finding(cweSessionURL, ctx.Target.URL, domain.SevMedium, "session identifier in URL")}, false, nil
	}
	return nil, false, nil
}

// ---- PS-022: crossdomain.xml permissive ----
type crossdomainSig struct{}

var crossdomainWild = regexp.MustCompile(`(?i)<allow-access-from[^>]+domain\s*=\s*["']\*["']`)

func (crossdomainSig) CWE() domain.CWE { return cweCrossdomain }
func (crossdomainSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil {
		return nil, false, nil
	}
	if crossdomainWild.Match(ctx.Response.Body) {
		return []domain.Finding{finding(cweCrossdomain, ctx.Target.URL, domain.SevMedium, `crossdomain.xml allows domain="*"`)}, false, nil
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
		return []domain.Finding{finding(cweBackup, ctx.Target.URL, domain.SevHigh, "accessible backup file")}, false, nil
	}
	return nil, false, nil
}

// ---- PS-013: admin page publicly accessible ----
type adminPageSig struct{}

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
func (adminPageSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil {
		return nil, false, nil
	}
	if !adminPath.MatchString(pathOf(ctx.Target.URL)) {
		return nil, true, nil // not an admin path → N/A for this target
	}
	// Public access = 200, an actual admin interface in the body, no login form,
	// and no login/SPA marker. The positive admin-content requirement is what stops
	// a soft-404 homepage from being reported as an open admin page.
	body := string(ctx.Response.Body)
	if ctx.Response.Status == 200 && adminContentMarker.MatchString(body) &&
		!hasPasswordField(body) && !loginMarker.MatchString(body) {
		return []domain.Finding{finding(cweAdmin, ctx.Target.URL, domain.SevHigh, "admin page reachable without authentication")}, false, nil
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
	if robotsSensitive.Match(ctx.Response.Body) {
		return []domain.Finding{finding(cweRobots, ctx.Target.URL, domain.SevLow, "robots.txt discloses sensitive path")}, false, nil
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
		return []domain.Finding{finding(cweCaptcha, ctx.Target.URL, domain.SevLow, "login form without CAPTCHA")}, false, nil
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
func cookieFinding(cwe domain.CWE, url, desc, first string, n int) []domain.Finding {
	ev := desc + ": " + first
	if n > 1 {
		ev = ev + " (+" + itoa(n-1) + " more)"
	}
	return []domain.Finding{finding(cwe, url, domain.SevMedium, ev)}
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
		return cookieFinding(cweCookieSecure, ctx.Target.URL, "cookie without Secure", first, n), false, nil
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
		return cookieFinding(cweCookieHTTPOnly, ctx.Target.URL, "cookie without HttpOnly", first, n), false, nil
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
		return cookieFinding(cweCookieSameSite, ctx.Target.URL, "cookie with weak SameSite (missing or None)", first, n), false, nil
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
