package observation

import (
	"strings"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

func ctxWithCookies(cs ...domain.Cookie) domain.ScanContext {
	return domain.ScanContext{
		Target:   domain.Target{URL: "https://a.test/"},
		Response: &domain.HAREntry{URL: "https://a.test/", Status: 200, Cookies: cs},
	}
}

// An insecure cookie (missing the attribute) yields a finding on the owning CWE.
func TestCookieSigs_Insecure(t *testing.T) {
	insecure := domain.Cookie{Name: "sid", Secure: false, HttpOnly: false, SameSite: ""}
	cases := []struct {
		name string
		sig  Signature
		cwe  domain.CWE
	}{
		{"secure", cookieSecureSig{}, cweCookieSecure},
		{"httponly", cookieHTTPOnlySig{}, cweCookieHTTPOnly},
		{"samesite", cookieSameSiteSig{}, cweCookieSameSite},
	}
	for _, c := range cases {
		fs, na, err := c.sig.Judge(ctxWithCookies(insecure))
		if err != nil || na {
			t.Errorf("%s: want a finding, got na=%v err=%v", c.name, na, err)
		}
		if len(fs) != 1 || fs[0].CWEs[0] != c.cwe || !fs[0].Severity.Valid() {
			t.Errorf("%s: want 1 %s finding, got %v", c.name, c.cwe, fs)
		}
	}
}

// A fully-secured cookie is clean (no finding, not N/A).
func TestCookieSigs_Secured(t *testing.T) {
	secure := domain.Cookie{Name: "sid", Secure: true, HttpOnly: true, SameSite: "Strict"}
	for _, sig := range []Signature{cookieSecureSig{}, cookieHTTPOnlySig{}, cookieSameSiteSig{}} {
		fs, na, err := sig.Judge(ctxWithCookies(secure))
		if err != nil || na || len(fs) != 0 {
			t.Errorf("%s: secured cookie must be clean, got fs=%v na=%v", sig.CWE(), fs, na)
		}
	}
}

// No cookies at all → N/A (nothing to judge), never a false finding.
func TestCookieSigs_NoCookies_NA(t *testing.T) {
	for _, sig := range []Signature{cookieSecureSig{}, cookieHTTPOnlySig{}, cookieSameSiteSig{}} {
		fs, na, err := sig.Judge(ctxWithCookies())
		if err != nil || !na || len(fs) != 0 {
			t.Errorf("%s: no cookies must be N/A, got fs=%v na=%v", sig.CWE(), fs, na)
		}
	}
}

// SameSite=None is as exposed as an absent SameSite.
func TestCookieSameSite_None_Flagged(t *testing.T) {
	fs, na, err := cookieSameSiteSig{}.Judge(ctxWithCookies(domain.Cookie{Name: "sid", Secure: true, HttpOnly: true, SameSite: "None"}))
	if err != nil || na || len(fs) != 1 {
		t.Fatalf("SameSite=None must be flagged, got fs=%v na=%v", fs, na)
	}
	// Evidence must not claim the cookie is "without SameSite" — it is present but
	// permissive (None). The message reflects both the missing and None cases.
	ev := fs[0].Evidence.SnippetStrings()[0]
	if strings.Contains(ev, "without SameSite") || !strings.Contains(ev, "missing or None") {
		t.Errorf("SameSite=None evidence must reflect the None case, got %q", ev)
	}
}
