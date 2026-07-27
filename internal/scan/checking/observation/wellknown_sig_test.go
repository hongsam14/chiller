package observation

import (
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

// FP/FN corpus for the r10 dictionary 심화 (C2=A). Every widened anchor must fire
// on the real weakness (FN) AND stay silent on the paired benign case (FP) — a
// broadened dictionary that also broadens false positives is not an improvement.
// Each row: signature, a body+URL, and whether a finding is expected.

func wctx(scheme string, status int, body, url string) domain.ScanContext {
	return domain.ScanContext{
		Target:   domain.Target{URL: url, Host: "h", Scheme: scheme},
		Response: &domain.HAREntry{Scheme: scheme, Status: status, Body: []byte(body), URL: url},
	}
}

// wctxCT is wctx with an explicit Content-Type header (used to prove a backup
// archive path served as text/html — a soft-404 — is not reported).
func wctxCT(scheme string, status int, body, url, contentType string) domain.ScanContext {
	c := wctx(scheme, status, body, url)
	c.Response.Headers = []domain.Header{{Name: "Content-Type", Value: contentType}}
	return c
}

func TestWellKnown_Corpus(t *testing.T) {
	cases := []struct {
		name string
		sig  Signature
		ctx  domain.ScanContext
		want bool // expect exactly one finding
	}{
		// ---- PS-011 .env: FN (real secret, prefixed/suffixed keys) ----
		{"env/FN stripe", envExposureSig{}, wctx("https", 200, "STRIPE_SECRET_KEY=sk_live_abc123def456\n", "https://h/.env"), true},
		{"env/FN jwt", envExposureSig{}, wctx("https", 200, "JWT_SECRET=s0meL0ngSecret\n", "https://h/.env"), true},
		{"env/FN mail", envExposureSig{}, wctx("https", 200, "MAIL_PASSWORD=hunter2x\n", "https://h/.env"), true},
		// ---- PS-011 .env: FN (original 4-name keys restored + short/weak real secrets) ----
		{"env/FN appkey", envExposureSig{}, wctx("https", 200, "APP_KEY=base64:aGVsbG93b3JsZDEyMzQ1Ng==\n", "https://h/.env"), true},
		{"env/FN short secret", envExposureSig{}, wctx("https", 200, "DB_PASSWORD=p@ss!\n", "https://h/.env"), true},
		{"env/FN weak real secret", envExposureSig{}, wctx("https", 200, "SECRET_KEY=secret\n", "https://h/.env"), true},
		{"env/FN weak real password", envExposureSig{}, wctx("https", 200, "DB_PASSWORD=password\n", "https://h/.env"), true},
		// ---- PS-011 .env: FP (framework .env.example placeholders / empty / not-a-.env-path) ----
		{"env/FP placeholder", envExposureSig{}, wctx("https", 200, "DB_PASSWORD=your_password_here\n", "https://h/.env"), false},
		{"env/FP empty", envExposureSig{}, wctx("https", 200, "APP_KEY=\nSECRET_KEY=\n", "https://h/.env"), false},
		{"env/FP changeme", envExposureSig{}, wctx("https", 200, "SECRET_KEY=changeme\n", "https://h/.env"), false},
		{"env/FP xxx", envExposureSig{}, wctx("https", 200, "PASSWORD=xxxxxxxx\n", "https://h/.env"), false},
		// path guard: the generalized key pattern must NOT fire on an arbitrary page.
		{"env/FP not-env-path", envExposureSig{}, wctx("https", 200, "API_TOKEN=abcdef123456\n", "https://h/config"), false},
		{"env/FP debug page", envExposureSig{}, wctx("https", 200, "access_token=deadbeefcafe1234\n", "https://h/debug"), false},
		// public template file (.env.example) holds placeholders by design → N/A.
		{"env/FP template", envExposureSig{}, wctx("https", 200, "SECRET_KEY=realvalue1234\n", "https://h/.env.example"), false},

		// ---- PS-012 backup: FN (backup-named, incl. separator-bearing names) ----
		{"backup/FN php.bak", backupFileSig{}, wctx("https", 200, "x", "https://h/index.php.bak"), true},
		{"backup/FN db.sql", backupFileSig{}, wctx("https", 200, "x", "https://h/db.sql"), true},
		{"backup/FN backup.zip", backupFileSig{}, wctx("https", 200, "x", "https://h/backup.zip"), true},
		{"backup/FN env.bak", backupFileSig{}, wctx("https", 200, "x", "https://h/.env.bak"), true},
		{"backup/FN site-backup", backupFileSig{}, wctx("https", 200, "x", "https://h/site-backup.zip"), true},
		{"backup/FN db_backup", backupFileSig{}, wctx("https", 200, "x", "https://h/db_backup.tar.gz"), true},
		{"backup/FN backup-2024", backupFileSig{}, wctx("https", 200, "x", "https://h/backup-2024.tar.gz"), true},
		{"backup/FN wwwroot", backupFileSig{}, wctx("https", 200, "x", "https://h/wwwroot.zip"), true},
		// ---- PS-012 backup: FP (ordinary archives, no backup keyword; soft-404 HTML) ----
		{"backup/FP report.zip", backupFileSig{}, wctx("https", 200, "x", "https://h/report.zip"), false},
		{"backup/FP data.sql", backupFileSig{}, wctx("https", 200, "x", "https://h/data.sql"), false},
		{"backup/FP photo.tar.gz", backupFileSig{}, wctx("https", 200, "x", "https://h/photo.tar.gz"), false},
		{"backup/FP sandbox.zip", backupFileSig{}, wctx("https", 200, "x", "https://h/sandbox.zip"), false},
		// soft-404: /backup.zip answered with 200 + homepage HTML must NOT self-fire.
		{"backup/FP soft404 body", backupFileSig{}, wctx("https", 200, "<!doctype html><html><body>Home</body></html>", "https://h/backup.zip"), false},
		{"backup/FP soft404 ct", backupFileSig{}, wctxCT("https", 200, "irrelevant", "https://h/db.sql", "text/html; charset=utf-8"), false},

		// ---- PS-010 .git: FN (config + HEAD) ----
		{"git/FN config", gitExposureSig{}, wctx("https", 200, "[core]\n\trepositoryformatversion = 0\n", "https://h/.git/config"), true},
		{"git/FN head", gitExposureSig{}, wctx("https", 200, "ref: refs/heads/main\n", "https://h/.git/HEAD"), true},
		// ---- PS-010 .git: FP (.gitignore / .github, loose-match bait) ----
		{"git/FP gitignore", gitExposureSig{}, wctx("https", 200, "node_modules\n[core]\n", "https://h/.gitignore"), false},
		{"git/FP github dir", gitExposureSig{}, wctx("https", 200, "[core] docs", "https://h/.github/workflows/ci.yml"), false},

		// ---- PS-013 admin: FN (open admin surfaces; incidental "login" prose must not suppress) ----
		{"admin/FN admin", adminPageSig{}, wctx("https", 200, "<h1>Admin Panel</h1><p>users</p>", "https://h/admin"), true},
		{"admin/FN phpmyadmin", adminPageSig{}, wctx("https", 200, "<title>phpMyAdmin</title>databases", "https://h/phpmyadmin/"), true},
		{"admin/FN lastlogin prose", adminPageSig{}, wctx("https", 200, "<h1>Admin Dashboard</h1><p>Last login: 2024-01-02</p><table>users</table>", "https://h/admin"), true},
		{"admin/FN loginlink nav", adminPageSig{}, wctx("https", 200, `<h1>Admin Console</h1><a href="/wp-login.php">help</a><div>settings</div>`, "https://h/admin"), true},
		// ---- PS-013 admin: FP (login form / SPA shell / sign-in text / soft-404 homepage) ----
		{"admin/FP loginform", adminPageSig{}, wctx("https", 200, `<h1>Admin Login</h1><form action="/login"><input type="password"></form>`, "https://h/admin"), false},
		{"admin/FP spa", adminPageSig{}, wctx("https", 200, `<html><body><div id="root"></div></body></html>`, "https://h/admin"), false},
		{"admin/FP signin", adminPageSig{}, wctx("https", 200, `<h1>Sign in to continue</h1>`, "https://h/admin"), false},
		{"admin/FP signin title", adminPageSig{}, wctx("https", 200, `<title>Sign in</title><h1>Admin Panel</h1>`, "https://h/admin"), false},
		// soft-404: a marketing homepage returned for /admin has no admin content → no fabricated High.
		{"admin/FP soft404 home", adminPageSig{}, wctx("https", 200, "<!doctype html><html><body><h1>Welcome to Acme</h1><p>We sell widgets.</p></body></html>", "https://h/admin"), false},

		// ---- PS-008 robots: FN (broadened keywords, as path segments) ----
		{"robots/FN database", robotsSensitiveSig{}, wctx("https", 200, "User-agent: *\nDisallow: /database/", "https://h/robots.txt"), true},
		{"robots/FN dotenv", robotsSensitiveSig{}, wctx("https", 200, "Disallow: /.env", "https://h/robots.txt"), true},
		{"robots/FN logs", robotsSensitiveSig{}, wctx("https", 200, "Disallow: /logs", "https://h/robots.txt"), true},
		{"robots/FN db segment", robotsSensitiveSig{}, wctx("https", 200, "Disallow: /db/dump", "https://h/robots.txt"), true},
		{"robots/FN api segment", robotsSensitiveSig{}, wctx("https", 200, "Disallow: /api/keys", "https://h/robots.txt"), true},
		// ---- PS-008 robots: FP (ordinary Disallow; keywords only as substrings, not segments) ----
		{"robots/FP public", robotsSensitiveSig{}, wctx("https", 200, "Disallow: /public\nDisallow: /images", "https://h/robots.txt"), false},
		{"robots/FP feedback", robotsSensitiveSig{}, wctx("https", 200, "Disallow: /feedback", "https://h/robots.txt"), false},
		{"robots/FP cardboard", robotsSensitiveSig{}, wctx("https", 200, "Disallow: /cardboard/", "https://h/robots.txt"), false},
		{"robots/FP rapid-delivery", robotsSensitiveSig{}, wctx("https", 200, "Disallow: /rapid-delivery", "https://h/robots.txt"), false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs, _, err := c.sig.Judge(c.ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := len(fs) > 0
			if got != c.want {
				t.Errorf("fire=%v want=%v (findings=%d)", got, c.want, len(fs))
			}
			// Property: any finding owns exactly the signature's single CWE.
			for _, f := range fs {
				if len(f.CWEs) != 1 || f.CWEs[0] != c.sig.CWE() {
					t.Errorf("finding must own exactly the sig CWE %s, got %v", c.sig.CWE(), f.CWEs)
				}
			}
		})
	}
}
