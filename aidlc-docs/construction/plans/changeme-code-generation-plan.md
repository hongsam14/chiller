# Code Generation Plan — changeme / PS-019 wiring (r19)

Unit `scan`. Executes the Functional Design. **Smoke-first**: changeme's Python version + flags + output
schema are unknowns (like subzy's `--vuln_list`), so Step 0 settles them against a real binary BEFORE any
argv/parser is written — no code is authored on an assumed schema.

## Step 0 — Early Docker smoke (de-risk, BEFORE writing argv/parser)
- [ ] S0.1 Build a throwaway image installing changeme (git clone ztgrace/changeme + deps). Confirm the
      **Python version** (r7 flagged Python2 heritage — verify it installs/runs under the image's Python3).
      **F7: if changeme is Python2-only, keeping the stub is the STRONG DEFAULT** — bundling an EOL
      interpreter into the hardened (non-root, read-only-rootfs) image is a security regression; report back
      rather than ship it.
- [ ] S0.2 `changeme --help`: record the real flags for **web/http only**, **single-attempt / no-retry**,
      **minimal creds** (fingerprint-matched, NOT `--all`), and **JSON file output**. If any bound is
      unsupported by a flag, plan to constrain the creds file instead; if it cannot be bounded safely, STOP
      and report (keep the stub — honest tool-failed floor).
- [ ] S0.3 **F2 — creds/fingerprint DB provisioning**: determine whether changeme ships its creds (YAML in
      the repo → baked with the clone, like humble) or needs an EXTERNAL DB fetched at runtime (→ this
      becomes a subzy-style operator-provisioned volume, a scope jump). Settle this BEFORE designing the
      Dockerfile — it decides whether Step 5 is a simple bake or a make-target+volume increment.
- [ ] S0.4 **F1 — applicability oracle**: changeme is fingerprint-driven (like subzy). Check whether its
      output distinguishes *"no service fingerprinted"* (→ genuine **N/A**) from *"service matched, creds
      failed"* (→ **clean**). If it does, prefer that signal for the N/A-vs-clean split; `hasLoginSurface`
      then serves as the well-known amplification pre-filter + the "limited wiring" safety gate, NOT the
      sole applicability oracle. Record which signal is authoritative.
- [ ] S0.5 Run changeme once against a benign local login page (a mock with a `type=password` form) and a
      formless page; capture the **real JSON output** for both → golden schema for the parser. NOTE (F3):
      a generic login form likely won't match any changeme fingerprint → to capture a real POSITIVE, mock a
      recognized product's login (e.g. a fake Tomcat/manager response changeme fingerprints) or accept that
      the positive is proven only by the golden-fixture unit test (subzy precedent).
- [ ] S0.6 Write findings into this plan (Python version, chosen flags, DB provisioning verdict,
      applicability signal, a captured JSON sample) before Step 2/3.

## Step 0 — RESULTS (smoke against ztgrace/changeme v1.2.3, commit 26fa2c3, 2026-07-29)

**Blockers (fit against the hardened-minimal image):**
- **B1 (hard) — Python 3.13+ incompatibility.** changeme's `scan_engine.py` **eager-imports every protocol
  scanner**, and `scanners/telnet.py` imports `telnetlib`, **removed from Python 3.13+ (PEP 594)**. The
  runtime base is `python:3.14-slim`, so changeme **cannot even import** without a `telnetlib` **stdlib
  monkey-patch/shim** or pinning the whole runtime to Python ≤3.12 (itself deprecated, gone in 3.13).
- **B2 (heavy) — ~1.02 GB + large attack surface.** requirements pull paramiko (SSH), pysnmp (SNMP),
  psycopg2/pyodbc/pymongo (DB), selenium + wants phantomjs (headless browser), shodan, sqlalchemy — all
  **eager-imported at startup** (no http-only import path). Baking this into a non-root/read-only-rootfs/
  cap-drop-ALL scanner image directly conflicts with SECURITY-09 (minimal image) for one bounded web check.
- **B3 (perf) — slow.** ~24s to fingerprint one unreachable target (123 http profiles × requests); ×6
  well-known login paths/host is heavy latency.

**Positives (the tool itself is bound-able):**
- Python3-source (parses; only SyntaxWarnings). **F7 resolved** — not Python2; the blocker is telnetlib, a
  different axis.
- **F2 resolved** — creds are baked in the repo (`creds/http/…`, protocol-categorized). No external DB.
- Runs WITHOUT a redis server (redis imported but not required for a single scan). Output JSON schema is
  **`{"results": [ … ]}`** (`{"results": []}` when clean) — the golden schema.
- Good bound flags: `--protocols http` (**the default**), `--output x.json`, `--name`/`--category` (minimal
  dict), `--timeout`, `--delay` (rate), `--noversion` (no phone-home), and **`--fingerprint`** = fingerprint
  WITHOUT submitting creds → a clean **F1 applicability oracle** (no service matched ⇒ N/A, no login attempt).

**Verdict:** the tool CAN be bounded functionally, but its **packaging fit is bad** (B1 stdlib shim + B2
~1GB/SSH·DB·browser surface + B3 latency). Per the plan's honest floor, this is a **STOP-and-report** —
the increment must not silently ship a 1GB, Python-shimmed, multi-protocol credential attacker into the
hardened image. **Decision escalated to the user (see below). Steps 1–6 are GATED on that decision.**

---

## Step 1 — Runner: the `applicable` N/A hook (FD-1)
- [ ] S1.1 Add `applicable func(ctx domain.ScanContext) (na []domain.CWE, ok bool)` to `runner` (probe.go).
- [ ] S1.2 In `Check`, after the `precheck` block and before building args:
      `if r.applicable != nil { if na, ok := r.applicable(ctx); !ok { return domain.CheckResult{NotApplicable: na}, nil } }`.
- [ ] S1.3 Unit test: an adapter with `applicable`→(cwes,false) returns N/A and does NOT invoke exec
      (assert via a fake exec that must not be called); →(nil,true) proceeds normally.

## Step 2 — changeme adapter + applicability + login-surface helper (FD-1, FD-3)
- [ ] S2.1 `parsers.go`: `hasLoginSurface(r *domain.HAREntry) bool` — `loginField.Match(r.Body)` (Body is
      []byte) OR a `WWW-Authenticate` header (`strings.EqualFold(h.Name, …)`). Local to probe (no
      observation import).
- [ ] S2.2 `parsers.go`: `changemeApplicable(ctx) ([]domain.CWE, bool)` — `Response==nil || !hasLoginSurface`
      → `(cwesDefCr, false)`; else `(nil, true)`. (F1: this stays the conservative "limited wiring" pre-gate;
      if S0.4 finds changeme reports "no service matched", the parser ALSO maps that to N/A so a
      fingerprint-driven miss is N/A, not a generous clean. Document the residual under-coverage of
      fingerprintable non-form surfaces — honest.)
- [ ] S2.3 `parsers.go`: `changemeOutArg(t domain.Target, outPath string) []string` using the Step-0 flags
      (web-only, single-attempt/no-retry, minimal creds, JSON→outPath, target `t.URL`).
- [ ] S2.4 `parsers.go`: `changemeParse(t domain.Target, data []byte) ([]domain.Finding, error)` —
      empty/absent→clean; accepted-default-cred record→`finding(CWE-798, t.URL, SevHigh, "<svc> default cred <user:pass> accepted")` (bounded, no secret dump); malformed→error (tool-failed).
- [ ] S2.5 `adapters.go`: `Changeme()` → `r := newWiredFile("probe:changeme","changeme",domain.URLScoped,changemeOutArg,changemeParse,cwesDefCr...); r.applicable = changemeApplicable;` (tighter timeout if warranted) `; return r`. Update the stale F5 stub comment.

## Step 3 — Well-known login coverage (FD-2)
- [ ] S3.1 `wellknown.go`: add `Login bool` to `WellKnownPath`; set `Login:true` on `/admin`,
      `/administrator`, `/manager/html`, `/wp-admin`, `/phpmyadmin`, `/dashboard` only. Doc comment: why
      the api-doc paths stay false.
- [ ] S3.2 `workitem.go`: add `LoginCandidate bool `bson:"loginCandidate,omitempty" json:"loginCandidate,omitempty"`` with a doc comment (fan-out is the only producer; old items default false).
- [ ] S3.3 `fanout.go:95`: set `LoginCandidate: p.Login` on the synthesized well-known item.
- [ ] S3.4 `exec.go`: `probeByID(cs []domain.Checker, id domain.CheckerID) domain.Checker` helper; in the
      well-known branch, after `observationForCWE`, `if wi.LoginCandidate { if cm := probeByID(e.urlScoped,"probe:changeme"); cm != nil { checkers = append(checkers, cm) } }`.

## Step 4 — Tests + fixtures
- [ ] S4.1 `parsers` golden-fixture unit test: the Step-0 JSON sample (positive)→CWE-798 finding; empty→
      clean; malformed→error. Fixture under `test/fixtures/` or inline.
- [ ] S4.2 `hasLoginSurface`/`changemeApplicable` table test: password-field body → applicable;
      WWW-Authenticate header → applicable; plain page / nil Response → N/A.
- [ ] S4.3 `exec` test: a `LoginCandidate` well-known item runs the owning admin sig **and** changeme; a
      non-login well-known item (api-docs) runs only the owning sig (changeme absent). Guard the FR-6 bound.
- [ ] S4.4 `fanout` test: the 6 admin login paths yield `LoginCandidate:true`, others false.
- [ ] S4.5 `integration_test.go`: prove real-exec-no-error against a mock login page. A real POSITIVE (F3)
      needs a product-fingerprint mock, not a generic form — add changeme to the opt-in `PROBE_FIXTURE_*`
      set with such a fixture IF feasible; otherwise the positive is carried by the golden-fixture unit test
      (S4.1), honestly (subzy precedent — don't fake a positive).
- [ ] S4.6 **F5** — grep for any boot-completeness / registry test asserting changeme is a stub, unwired,
      or "6 of 7 wired" (e.g. `ErrNotWired`, `Wired()` assertions, stub counts). Update to 7/7 wired.

## Step 5 — Dockerfile (FD-3, packaging)
- [ ] S5.1 Bundle changeme per Step 0 (git clone + PATH wrapper like humble/Corsy, or pipx). Pin a commit.
      Ensure it runs as the non-root uid on the read-only rootfs (HOME=/scratch) — smoke-verified.
- [ ] S5.2 Update `Changeme()`'s registry comment / any "6 of 7 wired" counts to "7/7".

## Step 5b — Documentation (FR-C4, F4)
- [ ] S5b.1 README: note changeme is the one **active credential-submitting** probe — bounded to
      web/http, fingerprint-matched default creds, single-attempt/no-retry, approved∩allowlist only, on
      submitted URLs + the ≤6 well-known login surfaces. State the **F6 limitation**: SPA/JS-rendered
      logins (no static password field) and non-fingerprinted forms are reported N/A, not clean.
- [ ] S5b.2 Operations production-readiness note: the active-probe envelope + the honest N/A semantics.

## Step 6 — Local gates
- [ ] S6.1 `go build ./...`, `go vet ./... -tags integration`, `gofmt -l`, `make arch-check`.
- [ ] S6.2 `go test -race ./...` (unit + non-integration).
- [ ] S6.3 `make gosec` (justify any `#nosec` for the outPath ReadFile, as subzy did), `make secrets`,
      `make signatures-check`.

## Notes / invariants
- No interface change (adapter/Checker unchanged; `applicable` is an internal runner field).
- honesty: tool-failed≠false-clean · no-login-form⇒N/A · empty⇒clean · drift⇒tool-failed.
- FR-6 amplification bound: changeme on ≤6 login items/host, single-attempt, N/A-on-no-form.
- If Step 0 proves changeme cannot be safely bounded, the honest fallback is to keep the stub and report —
  the increment does not ship an unbounded credential attacker.
