# Requirements — changeme / PS-019 default-credential wiring (r19)

**Increment**: wire the last unwired probe, `changeme` (PS-019 기본 자격증명 사용, CWE-798), so PS-019
is judged instead of always `tool-failed`. changeme is the ONLY probe that actively **submits
credentials** (login attempts), so its scope is bounded per the decisions below. Single unit = `scan`
(probe). No interface change.

## The tension (why r7 deferred it)

PS-019 cannot be judged passively — there is no read-only signal for "admin/admin still works"; a scanner
must actually try the credential. changeme (ztgrace/changeme) does exactly that: it submits KNOWN default
credentials against a target's login surface. That is the most active action in the whole scanner and sits
on the scope-decision's *"exploitation/fuzzing is the scope line"* boundary, with real account-lockout /
IDS-trigger risk. So the wiring is deliberately conservative.

## Decisions (Requirements Analysis, 2026-07-29)

- **Q1 = A — bounded wiring.** Wire changeme for **web/http default-credential checks only**, a **minimal
  default-cred set**, against **approved ∩ allowlist** hosts only (the existing gate), honoring the
  **per-host rate budget** and per-request timeout. It tries KNOWN default creds for identified services,
  not a brute-force/fuzz — the active surface is minimized to the scope-decision's "active but bounded"
  envelope (D1), staying inside the exploitation/fuzzing line.
- **Q2 = A — single attempt, no retry.** changeme runs its default-cred set **once** per target; **no retry
  loop, no brute-force beyond the default set**, minimizing account-lockout risk. (Distinct from the
  transient-error retries the fetcher does for read-only GETs — this is credential submission.)
- **Applicability (raised in review) — no login surface ⇒ N/A, not clean.** PS-019 cannot be judged on a
  page with no login form: reporting "clean" there is dishonest (nothing was tested). changeme runs only
  where a login surface exists; the absence of one is **NotApplicable** (the precondition is absent),
  mirroring the observation `hasPasswordField → N/A` pattern.
- **Coverage = B (raised in review) — reach login surfaces via the well-known sweep.** Scanning only a
  homepage would leave PS-019 almost always N/A (no login form). So changeme also runs on the **login-
  candidate well-known paths** the r10 provider already fetches (`/admin`, `/login`, `/wp-admin`,
  `/wp-login.php`, `/administrator`, `/manager/html`, `/phpmyadmin`, `/dashboard`), so a homepage scan
  still exercises PS-019 against likely login surfaces. This selectively extends r16 FR-6 (a well-known
  login item runs changeme in addition to its owning observation signature) — bounded by single-attempt +
  the N/A-on-no-form applicability above (a 404 login-candidate → N/A, a real login page → one default-cred
  try). Full coverage still needs login-page discovery (the carried discovery gap).

## Functional Requirements

- **FR-C1** — Wire `Changeme()` as a real adapter (argv/outArg + parser), replacing the stub. It runs
  changeme against the URL-scoped target for web/http default credentials only, single-attempt, honoring
  the approval gate + per-host rate + timeout (the safety envelope is unchanged; changeme egresses through
  the same trust boundary as the other probes).
- **FR-C2** — Bundle changeme in the image (git clone + PATH wrapper or pipx, matching humble/Corsy). Its
  Python version + packaging are resolved at Infrastructure Design / Build & Test (an early Docker smoke,
  as the subzy `--vuln_list` smoke de-risked r18).
- **FR-C3** — Parse changeme's output to a PS-019 (CWE-798) finding when a default credential is accepted
  (evidence = the service/URL + which default cred, bounded, no secret dump); clean when none accepted;
  `tool-failed` on a run error — honesty invariant (tool-failed ≠ false-clean), like the other probes.
- **FR-C4 (safety)** — changeme submits credentials ONLY to approved ∩ allowlist hosts (the gate), single
  attempt, no retry/brute-force, rate-limited. Its aggressiveness is bounded to the minimal default-cred
  set; it never fuzzes or escalates. The active nature is documented (README/scope note).

## Non-Functional

- **NFR-C1 (safety-baseline)** — the active-credential envelope: approved∩allowlist only, single-attempt,
  rate/timeout bound, minimal dict. No new outbound scope beyond the gate. The exploitation/fuzzing line is
  respected (bounded default-cred try, not a brute-forcer).
- **NFR-C2 (resiliency/honesty)** — a run error or missing binary → `tool-failed`, never false-clean (the
  ErrNotWired/precheck spirit); total, no panic.
- **Invariants** — probe adapter interface · 26-item partition · CWE ownership (changeme owns CWE-798) ·
  rate/gate/timeout envelope · non-root/read-only-rootfs hardening · the honest 26-partition (wiring
  changeme flips PS-019 from tool-failed to judged).

## Verification risks (settled at Infra Design / Build & Test — early Docker smoke)

- changeme's actual Python version + install path (Python2 heritage per r7 — verify current is Python3).
- The exact flags for **web-only + single-attempt + no-retry + minimal dict** (does changeme support them,
  or must the invocation/creds file be constrained?). If changeme cannot be bounded safely, fall back to a
  constrained creds file or reconsider (honest tool-failed stays the floor).
- changeme's output schema for FR-C3 parsing.

## Out of scope (carried)

- Non-web protocols (ssh/snmp/etc.) — web/http only (Q1).
- Brute-force / credential-spraying beyond the minimal default set (Q2).
- Discovery/subdomain scope (separate large increment).

## Extension compliance (applicable)

| Extension | Applicable | How |
|---|---|---|
| security-baseline | Yes | bounded active envelope (approved∩allowlist, single-attempt, rate) — the most safety-sensitive probe |
| resiliency-baseline | Yes | run error / no binary → tool-failed, never false-clean |
| property-based-testing | N/A (light) | parser has a golden-fixture test; no new algorithmic core |
