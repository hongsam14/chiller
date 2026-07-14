package observation

import (
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

func ctxWith(scheme string, status int, body string, targetURL string) domain.ScanContext {
	return domain.ScanContext{
		Target:   domain.Target{URL: targetURL, Scheme: scheme},
		Response: &domain.HAREntry{Scheme: scheme, Status: status, Body: []byte(body)},
	}
}

// Anchor property: positive fixture → 1 finding, benign fixture → 0.
func TestSignatures_Anchors(t *testing.T) {
	cases := []struct {
		name    string
		sig     Signature
		pos     domain.ScanContext
		neg     domain.ScanContext
		negIsNA bool
	}{
		{"plaintext-login", plaintextLoginSig{},
			ctxWith("http", 200, `<form><input type="password"></form>`, "http://a/login"),
			ctxWith("https", 200, `<form><input type="password"></form>`, "https://a/login"), false},
		{"sensitive-comment", sensitiveCommentSig{},
			ctxWith("https", 200, `<!-- api_key=SECRET123 -->`, "https://a/"),
			ctxWith("https", 200, `<!-- just a normal note -->`, "https://a/"), false},
		{"git-exposure", gitExposureSig{},
			ctxWith("https", 200, "ref: refs/heads/main", "https://a/.git/HEAD"),
			ctxWith("https", 404, "not found", "https://a/.git/HEAD"), false},
		{"env-exposure", envExposureSig{},
			ctxWith("https", 200, "DB_PASSWORD=hunter2\n", "https://a/.env"),
			ctxWith("https", 200, "GREETING=hello\n", "https://a/.env"), false},
		{"session-in-url", sessionInURLSig{},
			ctxWith("https", 200, "", "https://a/p?jsessionid=ABC"),
			ctxWith("https", 200, "", "https://a/p?page=1"), false},
		{"crossdomain", crossdomainSig{},
			ctxWith("https", 200, `<allow-access-from domain="*"/>`, "https://a/crossdomain.xml"),
			ctxWith("https", 200, `<allow-access-from domain="a.test"/>`, "https://a/crossdomain.xml"), false},
		{"backup-file", backupFileSig{},
			ctxWith("https", 200, "data", "https://a/db.sql"),
			ctxWith("https", 200, "data", "https://a/index.html"), false},
		{"admin-page", adminPageSig{},
			ctxWith("https", 200, "<h1>Dashboard</h1>", "https://a/admin/"),
			ctxWith("https", 200, "<h1>Home</h1>", "https://a/home"), true}, // non-admin path → N/A
		{"robots", robotsSensitiveSig{},
			ctxWith("https", 200, "User-agent: *\nDisallow: /admin/secret", "https://a/robots.txt"),
			ctxWith("https", 200, "User-agent: *\nDisallow: /public", "https://a/robots.txt"), false},
		{"captcha-absence", captchaAbsenceSig{},
			ctxWith("https", 200, `<form><input type="password"></form>`, "https://a/login"),
			ctxWith("https", 200, `<p>no form here</p>`, "https://a/about"), true}, // no login → N/A
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, na, err := c.sig.Judge(c.pos)
			if err != nil || na || len(f) != 1 {
				t.Errorf("positive: got findings=%d na=%v err=%v, want 1/false/nil", len(f), na, err)
			}
			if len(f) == 1 && (f[0].CWEs[0] != c.sig.CWE() || f[0].SourceTool != "observation:"+string(c.sig.CWE())) {
				t.Errorf("positive finding wrong CWE/source: %+v", f[0])
			}
			f2, na2, err2 := c.sig.Judge(c.neg)
			if err2 != nil || len(f2) != 0 {
				t.Errorf("negative: got findings=%d err=%v, want 0/nil", len(f2), err2)
			}
			if na2 != c.negIsNA {
				t.Errorf("negative N/A: got %v want %v", na2, c.negIsNA)
			}
		})
	}
}

func TestDefault_TenSignatures_DistinctCWEs(t *testing.T) {
	sigs := Default()
	if len(sigs) != 10 {
		t.Fatalf("want 10 observation signatures, got %d", len(sigs))
	}
	seen := map[domain.CWE]bool{}
	for _, s := range sigs {
		if seen[s.CWE()] {
			t.Errorf("duplicate owned CWE %s", s.CWE())
		}
		seen[s.CWE()] = true
	}
	// each adapts to a domain.Checker with a stable id + ownership
	for _, c := range Checkers(sigs) {
		if c.Scope() != domain.URLScoped {
			t.Errorf("observation checker %s must be URL-scoped", c.ID())
		}
	}
}

// The engine adapter isolates a signature error to its own checker (R7).
type failSig struct{}

func (failSig) CWE() domain.CWE { return "CWE-999" }
func (failSig) Judge(domain.ScanContext) ([]domain.Finding, bool, error) {
	return nil, false, errBoom
}

var errBoom = &boomErr{}

type boomErr struct{}

func (*boomErr) Error() string { return "boom" }

func TestEngine_SignatureErrorIsolated(t *testing.T) {
	c := AsChecker(failSig{})
	if _, err := c.Check(domain.ScanContext{}); err == nil {
		t.Error("a signature error must surface as this checker's error (→ tool-failed)")
	}
}
