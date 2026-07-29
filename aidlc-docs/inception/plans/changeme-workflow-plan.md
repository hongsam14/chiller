# Workflow Plan — changeme / PS-019 wiring (r19)

Single unit = `scan`. The increment wires the last stub probe (`changeme`, CWE-798) AND selectively
extends the r16 FR-6 well-known execution so changeme reaches login surfaces (decision B). Touches:
probe adapter + parser, exec well-known branch, wellknown paths, fan-out marker, domain WorkItem,
Dockerfile.

## Architecture grounding (read before design)

- `changeme` is a **URL-scoped probe** (`probe:changeme`, owns CWE-798). exec **already fetches the page**
  for every URL-scoped item (`exec.go:64` ①) and puts it in `sctx.Response`. So the adapter can decide
  applicability (login surface present?) from the **already-fetched Response — no extra I/O, no wasted
  login attempt on a formless page**.
- FR-6 (`exec.go:105`): a well-known item runs ONLY `observationForCWE(cwe)` — probes are dropped. For B,
  the **login-candidate** well-known items (the 6 PS-013 admin login surfaces: `/admin`, `/administrator`,
  `/manager/html`, `/wp-admin`, `/phpmyadmin`, `/dashboard`) must ALSO run changeme. The api-doc paths
  (`/v3/api-docs` …) are NOT login surfaces → changeme must NOT run there (they share `cweAdmin`, so the
  CWE alone can't distinguish — an explicit login marker is needed).

## Stage decisions

| Stage | Decision | Why |
|---|---|---|
| Functional Design | **YES** | login-surface applicability heuristic (reuse observation's login/password-field detector) · the well-known login marker + exec extension · single-attempt safety semantics · finding shape. Real design choices. |
| NFR Requirements/Design | **SKIP (folded)** | no new tech stack; the safety envelope (approved∩allowlist · rate · timeout · single-attempt · minimal dict) is the CORE of Functional Design and covered there. |
| Infrastructure Design | **SKIP (folded into Code Gen)** | changeme is baked into the image like humble/Corsy (no volume, unlike subzy). Packaging (Python version, install, flags, output schema) is resolved by an **early Docker smoke in Code Gen P1** — the subzy-`--vuln_list` de-risk pattern. |
| Code Generation | **YES (P1 plan + early smoke → P2)** | the wiring + parser + Dockerfile + exec/wellknown/fanout/domain edits. |
| Build & Test | **YES** | the in-image Docker smoke is load-bearing (Python version + web-only/no-retry flags + real output schema — settled against reality, not assumed). |
| Operations | **YES** | production-readiness log; PS-019 flips tool-failed→judged; the active-credential envelope documented. |

## Design shape (to be firmed in Functional Design)

1. **Applicability precheck (changeme adapter)** — inspect `sctx.Response`: no login surface (no password
   field / no `WWW-Authenticate`) → **N/A** for CWE-798, do NOT run the CLI. Login surface present → run
   changeme once (single-attempt). Response nil (shouldn't happen for URL-scoped) → tool-failed. Honesty:
   run error → tool-failed, never false-clean.
2. **Well-known login marker** — `WellKnownPath{…, Login bool}` for the 6 admin login surfaces; fan-out
   sets `WorkItem.LoginCandidate bool` (bson/json omitempty); exec well-known branch appends `changeme` to
   `observationForCWE(cwe)` when `wi.LoginCandidate`. Bounded: single-attempt + N/A-on-no-form (404 admin
   → N/A, real admin login → one default-cred try).
3. **Parser + finding** — changeme output → PS-019 (CWE-798) finding (service/URL + which default cred,
   bounded evidence, no secret dump). Golden-fixture unit test; real schema settled at Build & Test smoke.
4. **Dockerfile** — bundle changeme (git clone + PATH wrapper or pipx), Python3 (verify at smoke).

## Verification risks (settled at Code Gen early smoke / Build & Test)
changeme's Python version · web-only/single-attempt/no-retry flags (or a constrained creds file) · output
schema. If changeme can't be bounded safely → constrain the creds file or reconsider (tool-failed floor).

## Invariants
probe adapter interface · 26-item partition · CWE ownership (changeme owns CWE-798) · rate/gate/timeout
envelope · FR-6 amplification bound (changeme added ONLY on the 6 login items, single-attempt) ·
non-root/read-only-rootfs hardening · honesty (tool-failed ≠ false-clean; no-login-form ⇒ N/A ≠ clean).
