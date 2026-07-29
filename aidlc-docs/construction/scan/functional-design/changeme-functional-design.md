# Functional Design — changeme / PS-019 wiring (r19)

Unit `scan`. Wires `probe:changeme` (CWE-798) and extends the FR-6 well-known branch so changeme reaches
login surfaces (decision B). Grounded in the actual code: `exec.go:45-137`, `probe.go:88-154`,
`wellknown.go`, `signatures.go:89-92` (`hasPasswordField`), `checker.go` (`ScanContext`, `CheckResult`).

## FD-1 — Applicability: no login surface ⇒ N/A (NOT clean, NOT tool-failed)

The runner today returns **findings-or-error** only; `precheck` maps to **tool-failed**. N/A is a THIRD
outcome the runner cannot currently express. Add an optional hook:

```
// runner.applicable (optional): decides, from the already-fetched Response, whether the
// tool's precondition holds. Returns the CWEs to mark N/A when it does NOT — the runner
// then returns CheckResult{NotApplicable: na} WITHOUT running the CLI. Distinct from
// precheck (→ tool-failed): a missing PRECONDITION is N/A, a broken RUN is tool-failed.
applicable func(ctx domain.ScanContext) (na []domain.CWE, ok bool)
```

Runner change (`Check`, after the `precheck` block, before exec):
```
if r.applicable != nil {
    if na, ok := r.applicable(ctx); !ok {
        return domain.CheckResult{NotApplicable: na}, nil // precondition absent → N/A, no CLI run
    }
}
```
`exec.go:132` already folds `res.NotApplicable` into `out.NotApplicable[c.ID()]`, so no exec change for
the N/A plumbing.

**changeme's applicable** = "is there a login surface in the fetched Response?":
```
func changemeApplicable(ctx domain.ScanContext) ([]domain.CWE, bool) {
    if ctx.Response == nil || !hasLoginSurface(ctx.Response) {
        return cwesDefCr, false // CWE-798 N/A: no login form to test
    }
    return nil, true
}
```
`hasLoginSurface` is a small **local** helper in the probe package (probe must NOT import observation —
package decoupling). `HAREntry` (verified `domain/har.go`): `Body []byte`, `Headers []Header{Name,Value}`.
Login surface = a password input field in the body **OR** an HTTP auth challenge:
```
var loginField = regexp.MustCompile(`(?i)<input[^>]+type\s*=\s*["']?password`) // mirrors observation's passwordField
func hasLoginSurface(r *domain.HAREntry) bool {
    if r == nil { return false }
    if loginField.Match(r.Body) { return true }                                  // Body is []byte
    for _, h := range r.Headers { if strings.EqualFold(h.Name, "WWW-Authenticate") { return true } } // Basic/Digest realm
    return false
}
```
Honesty: the CLI is
run ONLY where a login surface exists → no wasted login attempt on a formless page, and a formless page is
N/A not a fabricated clean.

## FD-2 — Well-known login coverage (B): changeme on the 6 admin login surfaces

- `WellKnownPath{Path, CWE, Login bool}` — set `Login:true` on the 6 PS-013 admin **login** surfaces
  (`/admin`, `/administrator`, `/manager/html`, `/wp-admin`, `/phpmyadmin`, `/dashboard`). The api-doc
  paths (`/v3/api-docs` …) and all other well-known paths stay `Login:false` — changeme must not run on
  non-login surfaces even though they share `cweAdmin`.
- `domain.WorkItem.LoginCandidate bool` (`bson:"loginCandidate,omitempty" json:",omitempty"`) — set by the
  fan-out (the only producer of well-known items) from `WellKnownPath.Login`, alongside `WellKnown` +
  `WellKnownCWE`. Old items default false → changeme never runs on them (safe).
- `exec.go` well-known branch — after `checkers = observationForCWE(e.urlScoped, wi.WellKnownCWE)`, append
  changeme when `wi.LoginCandidate`:
  ```
  if wi.LoginCandidate {
      if cm := probeByID(e.urlScoped, "probe:changeme"); cm != nil {
          checkers = append(checkers, cm) // owning admin sig + changeme, on login surfaces only
      }
  }
  ```
  `probeByID` = a tiny lookup over `e.urlScoped` (the changeme adapter is already registered there).
- **Bound (FR-6 amplification)**: changeme is added on ≤6 items/host, single-attempt, and immediately N/A
  on a 404/formless login-candidate (the applicable check runs on the fetched Response). A real admin
  login page → exactly one default-cred attempt. The gate is respected for free: a well-known item is
  URL-scoped, so its fetch (`exec.go:64`) is gated — an unapproved/unreachable host returns early and
  changeme never executes.

## FD-3 — changeme adapter (wiring)

`Changeme()` moves from `newStub` to a real adapter with the N/A hook:
```
func Changeme() Adapter {
    r := newWiredFile("probe:changeme", "changeme", domain.URLScoped, changemeOutArg, changemeParse, cwesDefCr...)
    r.applicable = changemeApplicable
    return r
}
```
- **File-output** (JSON to a temp file, like corsy/subzy) — cleaner than stdout scraping.
- `changemeOutArg(t domain.Target, outPath string) []string` — invoke changeme against `t.URL` for **web
  default credentials only**, **single-attempt / no-retry**, **minimal creds set**, JSON → `outPath`. The
  EXACT flags (and whether changeme even supports web-only/no-retry, or the creds file must be constrained)
  are settled by the **Code Gen early Docker smoke** — the subzy `--vuln_list` precedent. If it can't be
  bounded, constrain the creds file; if still unsafe, keep the stub (honest tool-failed floor) and report
  back before finishing.
- Timeout: the runner default (30s) or a tighter changeme-specific value (login attempts should be quick).

## FD-4 — Parser + finding (FR-C3)

`changemeParse(t domain.Target, data []byte) ([]domain.Finding, error)`:
- Empty/absent output → no findings → **clean** (the tool ran, nothing accepted) — same contract as the
  other file-output probes (`probe.go:140`).
- A record indicating an accepted default credential → one `Finding{CWE-798, SevHigh}`, evidence =
  the service/URL + which default cred matched (e.g. `admin:admin`), **bounded — no full secret/cookie
  dump**.
- Malformed JSON (schema drift) → **error → tool-failed**, never a silent clean (integration_test.go
  catches drift). Exact struct shape = golden fixture, confirmed at the smoke.

## FD-5 — Safety envelope (NFR, folded)

approved∩allowlist (gate, via the URL-scoped fetch) · single-attempt / no-retry (FD-3 flags) · minimal
web-only creds · per-request timeout · changeme added on ≤6 login items/host · N/A-on-no-form avoids
needless attempts. changeme does its own outbound I/O (like every probe) — bounded by the above, not by
the fetcher's rate limiter; documented in the Operations/README active-probe note. No new outbound scope.

## FD-6 — Honesty (invariant)

`tool-failed ≠ false-clean` (run error / missing binary → tool-failed) · **no-login-form ⇒ N/A ≠ clean**
(FD-1) · empty output ⇒ clean (tool ran) · schema drift ⇒ tool-failed. PS-019 flips tool-failed→judged
where a login surface is reached, N/A where none is, honest everywhere.

## Touch list
`internal/scan/checking/probe/probe.go` (applicable hook) · `.../adapters.go` (Changeme wired) ·
`.../parsers.go` (changemeOutArg, changemeParse, hasLoginSurface, changemeApplicable) ·
`internal/scan/checking/observation/wellknown.go` (Login field + 6 marks) ·
`internal/domain/workitem.go` (LoginCandidate) · `internal/platform/orchestrator/fanout.go` (set it) ·
`internal/scan/exec/exec.go` (append changeme on LoginCandidate + probeByID) · `Dockerfile` (bundle) ·
tests + fixtures + README/Operations note.
