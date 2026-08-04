package observation

import (
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

// r16 FP/FN corpus for the config-driven signatures wired to the OSS-distilled
// rules (PS-009 secrets + internal disclosure, PS-018 URL params, PS-013 api-docs).
// It loads the SAME configs/signatures/ that ship (via the *Sig(t) helpers), so a
// rule-data regression is caught here, not only in production. Each signature must
// fire on the real weakness (FN row) and stay silent on the paired benign case (FP
// row) — the whole point of r16 was killing FPs WITHOUT losing the true positives.

// jsCtx builds a ScanContext carrying one discovered first-party JS asset.
func jsCtx(pageURL, jsURL, jsBody string) domain.ScanContext {
	return domain.ScanContext{
		Target:       domain.Target{URL: pageURL, Host: "h", Scheme: "https"},
		Response:     &domain.HAREntry{Scheme: "https", Status: 200, Body: []byte(""), URL: pageURL},
		DiscoveredJS: []*domain.HAREntry{{Scheme: "https", Status: 200, Body: []byte(jsBody), URL: jsURL}},
	}
}

// realJWT is a syntactically valid JWT that clears the gitleaks jwt rule's entropy
// gate and is NOT on any allowlist (unlike the AKIA...EXAMPLE documentation key).
const realJWT = "eyJhbGciOiJIUzI1NiJ9.eyJ1c2VyIjoiYWRtaW4ifQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJVadQssw5c"

func TestDistilled_PS009_Secrets(t *testing.T) {
	sig := commentSig(t)
	cases := []struct {
		name string
		ctx  domain.ScanContext
		want bool
	}{
		// ---- FN: real secret in an HTML comment / first-party JS ----
		{"FN jwt in comment", wctx("https", 200, "<!-- token="+realJWT+" -->", "https://h/"), true},
		{"FN jwt in first-party JS", jsCtx("https://h/", "https://h/app.js", `var t="`+realJWT+`";`), true},

		// ---- FP: minified library asset is allowlisted (the jQuery-FP root cause) ----
		// The SAME real-looking token in a library file must NOT fire — gitleaks'
		// global allowlist excludes jquery/bootstrap/swagger-ui bundles.
		{"FP secret in jquery.min.js", jsCtx("https://h/", "https://h/vendor/jquery.min.js", `var t="`+realJWT+`";`), false},
		{"FP secret in bootstrap.min.js", jsCtx("https://h/", "https://h/assets/bootstrap.min.js", `k="`+realJWT+`"`), false},

		// ---- FP: benign minified first-party JS (`password:!0`) has no high-entropy secret ----
		{"FP jquery-style password flag", jsCtx("https://h/", "https://h/app.js", `return{password:!0,persistent:!0,remember:!1};`), false},
		{"FP ordinary comment", wctx("https", 200, "<!-- build 2024-01-02, contact webmaster -->", "https://h/"), false},

		// ---- FP: an `apiKey = value` whose RHS is an unquoted VARIABLE REFERENCE,
		// not a literal secret (the kbland.kr KBSignCertPlus.js FP). gitleaks'
		// generic-api-key `^[a-zA-Z_.-]+$` allow excuses an identifier/member-
		// expression value; it only bites once the allow is evaluated against the
		// captured secret rather than the whole `apiKey = …` match. ----
		{"FP apiKey assigned from member expression", jsCtx("https://h/", "https://h/libs/KBSignCertPlus.js", `function f(){params.apiKey = KBSignCertPlus.apiKey;params.signPlanText = t;}`), false},
		{"FP apiKey assigned from identifier", jsCtx("https://h/", "https://h/app.js", `const key = config.apiKey;`), false},
		// ---- FN: a real quoted generic api key (digits ⇒ not the identifier allow) still fires ----
		{"FN quoted generic api key", jsCtx("https://h/", "https://h/app.js", `var c={apiKey:"AbCdEf0123456789ghIJ"};`), true},
	}
	runFireCases(t, sig, cases)
}

func TestDistilled_PS009_InternalDisclosure(t *testing.T) {
	sig := commentSig(t)
	cases := []struct {
		name string
		ctx  domain.ScanContext
		want bool
	}{
		// ---- FN (FR-10 / Gap B): internal infra leaked in a comment ----
		{"FN rfc1918 ip", wctx("https", 200, "<!-- upstream 10.0.1.5 -->", "https://h/"), true},
		{"FN ec2 host", wctx("https", 200, "<!-- origin ip-10-0-1-5.ec2.internal -->", "https://h/"), true},
		{"FN staging host (human #16)", wctx("https", 200, "<!-- api: stage-www.kbland.kr -->", "https://h/"), true},
		{"FN internal tld", wctx("https", 200, "<!-- svc: payments.internal -->", "https://h/"), true},
		{"FN staging fqdn in JS", jsCtx("https://h/", "https://h/app.js", `var api="https://staging.example.com/v1";`), true},
		// ---- FP: a dotted version number is not an RFC1918 address (Go boundary guard) ----
		{"FP version string", wctx("https", 200, `<!-- buildNumber 3.10.0.1.5 -->`, "https://h/"), false},
		{"FP public-ish octet run", wctx("https", 200, `<!-- seq 100.10.0.1.200 -->`, "https://h/"), false},
		// ---- FP: ordinary dev/qa JS tokens are not non-prod hostnames (NonProdHosts FQDN guard) ----
		{"FP dev-mode token", jsCtx("https://h/", "https://h/app.js", `if(config.mode==="dev-mode"){enable();}`), false},
		{"FP dev.js asset ref", jsCtx("https://h/", "https://h/app.js", `import x from "./dev.js";`), false},
		{"FP qa-build comment", wctx("https", 200, "<!-- built by qa-build pipeline -->", "https://h/"), false},
	}
	runFireCases(t, sig, cases)
}

func TestDistilled_PS018_SessionInURL(t *testing.T) {
	sig := sessionSig(t)
	cases := []struct {
		name string
		ctx  domain.ScanContext
		want bool
	}{
		// ---- FN: sensitive-named param carrying a real value (human #3 accessToken) ----
		{"FN access_token", wctx("https", 200, "", "https://h/cb?access_token=abcdef123456"), true},
		{"FN accessToken camel", wctx("https", 200, "", "https://h/cb?accessToken=abcdef123456"), true},
		{"FN jsessionid", wctx("https", 200, "", "https://h/p?jsessionid=ABC123XYZ789"), true},
		{"FN password", wctx("https", 200, "", "https://h/p?password=hunter2x"), true},
		{"FN phpsessid", wctx("https", 200, "", "https://h/p?PHPSESSID=ab12cd34ef56"), true},
		{"FN sid", wctx("https", 200, "", "https://h/p?sid=ab12cd34ef56"), true},
		{"FN jsessionid matrix path-param", wctx("https", 200, "", "https://h/page;jsessionid=ABC123DEF456"), true},
		// ---- FP: short value / non-sensitive param ----
		{"FP short value", wctx("https", 200, "", "https://h/p?token=1"), false},
		{"FP non-sensitive param", wctx("https", 200, "", "https://h/p?ref=abcdef123456"), false},
		{"FP page param", wctx("https", 200, "", "https://h/p?page=2"), false},
		// ---- FP: benign params that merely CONTAIN a sensitive substring (token vs substring) ----
		{"FP userType substring", wctx("https", 200, "", "https://h/p?userType=standard"), false},
		{"FP passengers substring", wctx("https", 200, "", "https://h/p?passengers=Chennai"), false},
		{"FP boarding_pass substring", wctx("https", 200, "", "https://h/p?boarding_pass=ABC123"), false},
	}
	runFireCases(t, sig, cases)
}

func TestDistilled_PS013_APIDocs(t *testing.T) {
	sig := adminSig(t)
	cases := []struct {
		name string
		ctx  domain.ScanContext
		want bool
	}{
		// ---- FN: api-doc path + confirming body marker (human #10) ----
		{"FN v2 api-docs swagger", wctx("https", 200, `{"swagger":"2.0","paths":{}}`, "https://h/v2/api-docs"), true},
		{"FN swagger.json openapi", wctx("https", 200, `{"openapi":"3.0.0"}`, "https://h/swagger.json"), true},
		{"FN swagger-ui html", wctx("https", 200, `<div id="swagger-ui"></div>`, "https://h/swagger-ui.html"), true},
		// ---- FP: api-doc path but body does not confirm (soft-404 homepage) ----
		{"FP soft404 on api-docs path", wctx("https", 200, "<!doctype html><html><body>Welcome</body></html>", "https://h/v2/api-docs"), false},
		// ---- FP: swagger body served at a non-api-doc path → not this signature's target ----
		{"FP swagger body wrong path", wctx("https", 200, `{"swagger":"2.0"}`, "https://h/home"), false},
		// ---- FP: generic /docs page carrying "application/json" → generic path+marker dropped ----
		{"FP generic docs page", wctx("https", 200, `<html><body>API returns application/json</body></html>`, "https://h/docs"), false},
	}
	runFireCases(t, sig, cases)
}

// runFireCases asserts each ctx fires (or not) and that any finding owns exactly
// the signature's single CWE (the honesty invariant carried from the well-known
// corpus).
func runFireCases(t *testing.T, sig Signature, cases []struct {
	name string
	ctx  domain.ScanContext
	want bool
}) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs, _, err := sig.Judge(c.ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := len(fs) > 0; got != c.want {
				t.Errorf("fire=%v want=%v (findings=%d)", got, c.want, len(fs))
			}
			for _, f := range fs {
				if len(f.CWEs) != 1 || f.CWEs[0] != sig.CWE() {
					t.Errorf("finding must own exactly %s, got %v", sig.CWE(), f.CWEs)
				}
			}
		})
	}
}
