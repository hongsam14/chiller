package observation

import (
	"strings"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

// A finding must carry the ACTUAL matched data as evidence, not just a label, so a
// human or an AI can judge whether it is a real exposure or a false positive.
func TestEvidence_CarriesRealMatchedData(t *testing.T) {
	// A realistic JWT (not the AKIA doc example, which gitleaks allowlists).
	jsCtx := domain.ScanContext{
		Target:       domain.Target{URL: "https://h/app.js", Host: "h", Scheme: "https"},
		DiscoveredJS: []*domain.HAREntry{{URL: "https://h/app.js", Body: []byte(`var t="eyJhbGciOiJIUzI1NiJ9.eyJ1c2VyIjoiYWRtaW4ifQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJVadQssw5c";`)}},
	}
	cases := []struct {
		name string
		sig  Signature
		ctx  domain.ScanContext
		want string // a substring the evidence must contain (the real data)
	}{
		{"PS-009 JS token", commentSig(t), jsCtx, "eyJhbGciOiJIUzI1NiJ9"},
		{"PS-011 env secret", envExposureSig{}, wctx("https", 200, "STRIPE_SECRET_KEY=sk_live_abc123def456\n", "https://h/.env"), "sk_live_abc123def456"},
		{"PS-010 git config", gitExposureSig{}, wctx("https", 200, "[core]\n\trepositoryformatversion = 0\n", "https://h/.git/config"), "repositoryformatversion"},
		{"PS-008 robots line", robotsSensitiveSig{}, wctx("https", 200, "Disallow: /admin/secret", "https://h/robots.txt"), "/admin/secret"},
		{"PS-022 crossdomain", crossdomainSig{}, wctx("https", 200, `<allow-access-from domain="*"/>`, "https://h/crossdomain.xml"), "allow-access-from"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs, _, err := c.sig.Judge(c.ctx)
			if err != nil || len(fs) != 1 {
				t.Fatalf("want 1 finding, got %d (err %v)", len(fs), err)
			}
			ev := strings.Join(fs[0].Evidence.Snippets, " ")
			if !strings.Contains(ev, c.want) {
				t.Errorf("evidence must contain the real matched data %q, got %q", c.want, ev)
			}
		})
	}
}

// Evidence must stay bounded (FR-12/NFR-16) — a huge body cannot dump into it.
func TestEvidence_Bounded(t *testing.T) {
	big := "[core]\n" + strings.Repeat("A", 5000)
	fs, _, _ := gitExposureSig{}.Judge(wctx("https", 200, big, "https://h/.git/config"))
	if len(fs) != 1 {
		t.Fatal("expected a finding")
	}
	if n := len([]rune(fs[0].Evidence.Snippets[0])); n > maxEvidenceLen+64 {
		t.Errorf("evidence snippet not bounded: %d runes", n)
	}
}
