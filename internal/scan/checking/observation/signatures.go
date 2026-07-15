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
func (gitExposureSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil || ctx.Response.Status != 200 {
		return nil, false, nil
	}
	// Require a .git path — content anchors alone ("[core]", "ref: refs/") appear
	// in ordinary ini/docs pages and would false-positive without this.
	if !strings.Contains(pathOf(ctx.Target.URL), "/.git") {
		return nil, true, nil // not a .git endpoint → N/A for this target
	}
	b := string(ctx.Response.Body)
	if strings.HasPrefix(b, "ref: refs/") || strings.Contains(b, "[core]") {
		return []domain.Finding{finding(cweGit, ctx.Target.URL, domain.SevHigh, ".git metadata exposed")}, false, nil
	}
	return nil, false, nil
}

// ---- PS-011: .env exposure ----
type envExposureSig struct{}

var envAnchor = regexp.MustCompile(`(?m)^\s*(DB_PASSWORD|APP_KEY|SECRET_KEY|AWS_SECRET_ACCESS_KEY)\s*=`)

func (envExposureSig) CWE() domain.CWE { return cweEnv }
func (envExposureSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil || ctx.Response.Status != 200 {
		return nil, false, nil
	}
	if envAnchor.Match(ctx.Response.Body) {
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

// ---- PS-012: backup file exposure (URL-suffix anchor) ----
type backupFileSig struct{}

var backupSuffix = regexp.MustCompile(`(?i)\.(bak|old|zip|tar|tar\.gz|sql|swp)$|~$`)

func (backupFileSig) CWE() domain.CWE { return cweBackup }
func (backupFileSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil || ctx.Response.Status != 200 {
		return nil, false, nil
	}
	if backupSuffix.MatchString(pathOf(ctx.Target.URL)) {
		return []domain.Finding{finding(cweBackup, ctx.Target.URL, domain.SevHigh, "accessible backup file")}, false, nil
	}
	return nil, false, nil
}

// ---- PS-013: admin page publicly accessible ----
type adminPageSig struct{}

var adminPath = regexp.MustCompile(`(?i)/(admin|administrator|manage|wp-admin)(/|$)`)

func (adminPageSig) CWE() domain.CWE { return cweAdmin }
func (adminPageSig) Judge(ctx domain.ScanContext) ([]domain.Finding, bool, error) {
	if ctx.Response == nil {
		return nil, false, nil
	}
	if !adminPath.MatchString(pathOf(ctx.Target.URL)) {
		return nil, true, nil // not an admin path → N/A for this target
	}
	// public access = 200 and not a login/redirect
	if ctx.Response.Status == 200 && !hasPasswordField(string(ctx.Response.Body)) {
		return []domain.Finding{finding(cweAdmin, ctx.Target.URL, domain.SevHigh, "admin page reachable without authentication")}, false, nil
	}
	return nil, false, nil
}

// ---- PS-008: robots.txt sensitive path (light anchor) ----
type robotsSensitiveSig struct{}

var robotsSensitive = regexp.MustCompile(`(?im)^\s*Disallow:\s*/.*(admin|backup|config|secret|private|internal)`)

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

func cookieFinding(cwe domain.CWE, url, attr, first string, n int) []domain.Finding {
	ev := "cookie without " + attr + ": " + first
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
		return cookieFinding(cweCookieSecure, ctx.Target.URL, "Secure", first, n), false, nil
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
		return cookieFinding(cweCookieHTTPOnly, ctx.Target.URL, "HttpOnly", first, n), false, nil
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
		return cookieFinding(cweCookieSameSite, ctx.Target.URL, "SameSite", first, n), false, nil
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
