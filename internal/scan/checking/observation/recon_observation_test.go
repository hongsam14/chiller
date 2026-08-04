package observation

import (
	"strings"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

// FP/FN corpus for the r21 recon-distilled observation work: backup magic (PS-012),
// .DS_Store (PS-010), product exposures (PS-013), stack trace (PS-006), error info
// (PS-005), PII (CWE-359, 26-외). Each row must fire on the real weakness (FN) AND
// stay silent on the paired benign case (FP).

// zipMagic / sqliteMagic / dsStoreMagic are the minimal real artifact prefixes.
var (
	zipMagic     = "PK\x03\x04" + strings.Repeat("\x00", 20)
	sqliteMagic  = "SQLite format 3\x00" + strings.Repeat("\x00", 20)
	sqlDumpMagic = "-- MySQL dump 10.13  Distrib 8.0.32\nCREATE TABLE users(...);"
	dsStoreMagic = "\x00\x00\x00\x01Bud1\x00\x00" + strings.Repeat("\x00", 16)
)

func TestBackupMagic_Corpus(t *testing.T) {
	sig := backupFileSig{}
	cases := []struct {
		name string
		ctx  domain.ScanContext
		want bool
	}{
		{"FN zip archive at backup name", wctx("https", 200, zipMagic, "https://h/backup.zip"), true},
		{"FN sqlite db at backup name", wctx("https", 200, sqliteMagic, "https://h/db.sql"), true},
		{"FN sql dump at backup name", wctx("https", 200, sqlDumpMagic, "https://h/db.sql"), true},
		// FP: a legit download asset that is NOT backup-named must never fire, even on real zip magic.
		{"FP non-backup zip download", wctx("https", 200, zipMagic, "https://h/assets/report.zip"), false},
		// FP: backup-named path that returns the homepage HTML (soft-404) — no archive magic.
		{"FP soft-404 html at backup name", wctxCT("https", 200, "<!doctype html><html>home</html>", "https://h/backup.zip", "text/html"), false},
	}
	runCorpus(t, sig, cases)
}

func TestDSStore_Corpus(t *testing.T) {
	sig := gitExposureSig{}
	cases := []struct {
		name string
		ctx  domain.ScanContext
		want bool
	}{
		{"FN real .DS_Store (Bud1 magic)", wctx("https", 200, dsStoreMagic, "https://h/.DS_Store"), true},
		{"FP .DS_Store name but html", wctxCT("https", 200, "<!doctype html><html>home</html>", "https://h/.DS_Store", "text/html"), false},
		// The existing .git branch still works alongside the new .DS_Store branch.
		{"FN .git/HEAD", wctx("https", 200, "ref: refs/heads/main\n", "https://h/.git/HEAD"), true},
	}
	runCorpus(t, sig, cases)
}

func TestProductExposure_Corpus(t *testing.T) {
	sig := adminSig(t)
	cases := []struct {
		name string
		ctx  domain.ScanContext
		want bool
	}{
		{"FN actuator env json", wctx("https", 200, `{"activeProfiles":["prod"],"propertySources":[]}`, "https://h/actuator/env"), true},
		{"FN phpinfo page", wctx("https", 200, "<html><title>phpinfo()</title>PHP Version 8.2.1 PHP Extension</html>", "https://h/phpinfo.php"), true},
		// FP: the exposure path served a homepage (no product marker in the body).
		{"FP soft-404 at actuator path", wctx("https", 200, "<!doctype html><html>marketing home</html>", "https://h/actuator/env"), false},
	}
	runCorpus(t, sig, cases)
}

func TestStackTrace_Corpus(t *testing.T) {
	sig := stackSig(t)
	cases := []struct {
		name string
		ctx  domain.ScanContext
		want bool
	}{
		{"FN python traceback", wctx("https", 500, "Traceback (most recent call last):\n  File \"/app/x.py\", line 42, in handler", "https://h/boom"), true},
		{"FN java stack frame", wctx("https", 500, "java.lang.NullPointerException\n\tat com.acme.Svc.run(Svc.java:88)", "https://h/boom"), true},
		{"FN laravel whoops", wctx("https", 500, "<title>Whoops! There was an error.</title>", "https://h/boom"), true},
		// FP: an ordinary page merely mentioning the word "error" must not fire.
		{"FP benign error prose", wctx("https", 200, "<html>Sorry, an error occurred. Please try again.</html>", "https://h/"), false},
	}
	runCorpus(t, sig, cases)
}

func TestErrorInfo_Corpus(t *testing.T) {
	sig := errInfoSig(t)
	cases := []struct {
		name string
		ctx  domain.ScanContext
		want bool
	}{
		{"FN django debug urlconf", wctx("https", 404, "Using the URLconf defined in myproject.urls, Django tried these URL patterns, in this order", "https://h/nope"), true},
		{"FP benign 404", wctx("https", 404, "<html>Page not found</html>", "https://h/nope"), false},
	}
	runCorpus(t, sig, cases)
}

func TestPII_Corpus(t *testing.T) {
	sig := piiExposureSig{}
	cases := []struct {
		name string
		ctx  domain.ScanContext
		want bool
	}{
		{"FN korean rrn", wctx("https", 200, `{"user":"kim","rrn":"900101-1234567"}`, "https://h/api/me"), true},
		{"FN email field", wctx("https", 200, `{"email":"jane@example.com"}`, "https://h/api/me"), true},
		{"FN phone field", wctx("https", 200, `{"phone":"010-1234-5678"}`, "https://h/api/me"), true},
		// FN: compound key names (the project's own userEmail convention, mobilePhone).
		{"FN compound userEmail key", wctx("https", 200, `{"userEmail":"jane@corp.com"}`, "https://h/api/me"), true},
		{"FN compound emailAddress key", wctx("https", 200, `{"emailAddress":"jane@corp.com"}`, "https://h/api/me"), true},
		{"FN compound mobilePhone key", wctx("https", 200, `{"mobilePhone":"010-1234-5678"}`, "https://h/api/me"), true},
		// FP: a random 13-digit run that is not an RRN (7th digit 5 ∉ 1..4, or embedded).
		{"FP non-rrn digit run", wctx("https", 200, `{"seq":"9001010789012"}`, "https://h/api"), false},
		// FP: a dashed id whose YYMMDD prefix is not a valid birthdate (month 24).
		{"FP dashed non-rrn (bad month)", wctx("https", 200, `{"invoice":"202401-1000042"}`, "https://h/api"), false},
		// FP: benign field names that resemble PII keys (telemetry contains 'tel').
		{"FP userType not pii", wctx("https", 200, `{"userType":"admin","passengers":3}`, "https://h/api"), false},
		{"FP telemetry not phone", wctx("https", 200, `{"telemetry":"012345678"}`, "https://h/api"), false},
	}
	runCorpus(t, sig, cases)
}

// runCorpus asserts each case yields exactly one finding (want=true) or none.
func runCorpus(t *testing.T, sig Signature, cases []struct {
	name string
	ctx  domain.ScanContext
	want bool
}) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, _, err := sig.Judge(c.ctx)
			if err != nil {
				t.Fatalf("judge error: %v", err)
			}
			if got := len(f) == 1; got != c.want {
				t.Fatalf("findings=%d, want fire=%v", len(f), c.want)
			}
			if c.want {
				if f[0].CWEs[0] != sig.CWE() {
					t.Errorf("finding CWE=%s, want owner %s", f[0].CWEs[0], sig.CWE())
				}
			}
		})
	}
}

// ---- PBT: PII value masking + evidence offset integrity (FR-5c, PD-E/H2) ----

// TestPII_ValueNeverInSnippet proves the actual personal datum never appears in the
// evidence snippet, while the offset/length still index the ORIGINAL body so a
// downstream consumer can re-locate the real value.
func TestPII_ValueNeverInSnippet(t *testing.T) {
	rrn := "900101-1234567"
	email := "secret.person@corp.example.com"
	for _, tc := range []struct {
		body   string
		secret string
	}{
		{`{"rrn":"` + rrn + `"}`, rrn},
		{`{"email":"` + email + `"}`, email},
		// colon inside the value must not leak the local-part fragment (maskPII fix).
		{`{"email":"a:` + email + `"}`, "a:" + email},
	} {
		ctx := wctx("https", 200, tc.body, "https://h/api/me")
		f, _, _ := piiExposureSig{}.Judge(ctx)
		if len(f) != 1 {
			t.Fatalf("expected a PII finding for %q", tc.body)
		}
		it := f[0].Evidence.Items[0]
		if strings.Contains(it.Snippet, tc.secret) {
			t.Errorf("evidence snippet leaked the raw PII value: %q", it.Snippet)
		}
		// Offset/length must index the original body at the real value.
		body := []byte(tc.body)
		if it.Location.Offset < 0 || it.Location.Offset+it.Location.MatchLen > len(body) {
			t.Fatalf("offset/len out of bounds: off=%d len=%d body=%d", it.Location.Offset, it.Location.MatchLen, len(body))
		}
		got := string(body[it.Location.Offset : it.Location.Offset+it.Location.MatchLen])
		if !strings.Contains(got, tc.secret) {
			t.Errorf("original-body slice at offset does not contain the value: got %q want to contain %q", got, tc.secret)
		}
	}
}

// TestReconSigs_EvidenceOffsetBounds proves every r21 body-offset finding indexes
// within the response body (boundary property).
func TestReconSigs_EvidenceOffsetBounds(t *testing.T) {
	cases := []struct {
		sig Signature
		ctx domain.ScanContext
	}{
		{backupFileSig{}, wctx("https", 200, zipMagic, "https://h/backup.zip")},
		{gitExposureSig{}, wctx("https", 200, dsStoreMagic, "https://h/.DS_Store")},
		{stackSig(t), wctx("https", 500, "Traceback (most recent call last):\n  File \"/a/x.py\", line 9, in f", "https://h/x")},
		{piiExposureSig{}, wctx("https", 200, `{"email":"ab@b.co"}`, "https://h/x")},
	}
	for _, c := range cases {
		f, _, _ := c.sig.Judge(c.ctx)
		if len(f) != 1 {
			t.Fatalf("%s: expected a finding", c.sig.CWE())
		}
		loc := f[0].Evidence.Items[0].Location
		if loc.Part == domain.PartBody {
			blen := len(c.ctx.Response.Body)
			if loc.Offset < 0 || loc.Offset > blen || loc.Offset+loc.MatchLen > blen {
				t.Errorf("%s: body offset out of bounds off=%d len=%d body=%d", c.sig.CWE(), loc.Offset, loc.MatchLen, blen)
			}
		}
	}
}
