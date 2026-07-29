# Infrastructure Design — subzy fingerprints DB provisioning (#0e, r18)

Unit `scan` (probe). Implements `requirements-subzy-db.md`. No new deployable; a vendored
data asset + a Dockerfile `COPY` + a one-line `argv` change, fit to the existing hardened
runtime image (non-root, read-only rootfs, `/scratch` emptyDir).

## IS1 — Provisioning target (FR-S1) — operator-owned, not vendored

- **`make subzy-db`** — fetches `fingerprints.json` at a **pinned commit** to a **git-ignored** host
  path `deploy/subzy/fingerprints.json` (add to `.gitignore`). It does NOT commit the DB to the repo.
- **Source (lowest schema risk):** the `fingerprints.json` that **subzy v1.1.0 itself expects** — from
  **`LukaSikic/subzy` at the `v1.1.0` tag** (the exact binary the image installs, so `--vuln_list` is
  guaranteed to parse it). Upstream provenance is EdOverflow/can-i-take-over-xyz. The target pins the
  raw URL + commit and prints the provenance; `curl` @ the pinned ref (reliable, r16 method).
- The fetch is a **one-time provision step** (operator/CI), not per-run — a dead/moved URL fails loudly
  here, before deploy, and is fixable by bumping the pin. This is the key difference from #0e.
- **Verification risk:** if subzy v1.1.0 does not honor `--vuln_list` or expects a different schema,
  the fallback is to mount the same file at subzy's `$HOME/subzy/` cache path instead (Q2-B) — settled
  at Build & Test on a real Docker run.

## IS2 — Injection via read-only volume (FR-S2) — Dockerfile does NOT bake

- The runtime consumes the DB via a **read-only volume mount** at `SUBZY_VULN_LIST` (default
  `/opt/subzy/fingerprints.json`). The Dockerfile only sets `ENV SUBZY_VULN_LIST=/opt/subzy/fingerprints.json`
  (+ a doc comment); it copies no data. Read-only mount fits the read-only rootfs.
- `deploy/compose/docker-compose.yml` — mount `./deploy/subzy/fingerprints.json:/opt/subzy/fingerprints.json:ro`.
- `deploy/k8s` — a volume from a ConfigMap/Secret the operator provisions (documented in
  `secret.example.yaml`-style form), mirroring how storageState / secrets are already handled.
- Absent mount → subzy self-fetches → fails → PS-025 `tool-failed` (honest; NFR-S1).

## IS3 — Invocation wiring (FR-S3) — REVISED at Code Gen (subzy has NO fingerprint flag)

**Code-Gen smoke finding (P3 early smoke paid off):** `subzy v1.1.0 run --help` has **no**
`--vuln_list`/fingerprint-path flag (only `--target/--targets/--output/--hide_fails/--vuln/…`). subzy
loads its DB from the hard-coded **`$HOME/subzy/fingerprints.json`** and self-fetches only if it is
absent. Verified in a throwaway container: with the DB mounted read-only at `$HOME/subzy/fingerprints.json`
subzy runs clean (exit 0, parseable JSON, **no self-fetch**). So Q2-A (`--vuln_list`) is impossible and
the design falls back to **Q2-B (the DB at subzy's `$HOME/subzy/` path)** — which the r9 CI already proved.

- The DB is mounted read-only at **`$SUBZY_DB`** (default `/opt/subzy/fingerprints.json`); the subzy
  adapter runs subzy with **`HOME=<dir(dir(SUBZY_DB))>`** (default `/opt`) via `runner.extraEnv`, so
  subzy's `$HOME/subzy/fingerprints.json` resolves to the mount.
- `subzyOutArg` carries **no** fingerprint flag. A local/integration run overrides `SUBZY_DB` to the
  fetched `deploy/subzy/fingerprints.json`.
- Net effect: subzy reads the provisioned DB and **never self-fetches** — one fewer runtime outbound
  (net-negative attack surface, NFR-S2).

## IS4 — Pin / refresh policy (Q3)

- `make subzy-db` pins the raw URL + commit SHA (the single source of truth for the version). Refresh =
  bump the pin + re-run the target (operator responsibility). A CI auto-refresh + drift job is a
  documented follow-up, out of scope.
- No repo-committed DB, so no `make secrets` exposure from the data; `deploy/subzy/` is git-ignored.

## IS5 — Verification (FR-S4) — the load-bearing gate

- **Local**: a `subzyOutArg` test asserts the argv includes `--vuln_list` + the env-default path.
- **Integration (opt-in, r9 pattern)**: `make subzy-db` then `PROBE_FIXTURE_SUBZY=takeover-mock` with
  `SUBZY_VULN_LIST=deploy/subzy/fingerprints.json` → subzy runs against the fixture's takeover body
  **without the self-fetch/parse error** and yields a **PS-025 finding**; a benign host → clean.
- **In-image (Docker)**: build the image, mount the DB, run subzy against the vuln-target `takeover-mock`
  — confirms the volume + `--vuln_list` flow end-to-end (where the flag risk is finally settled).

## Compliance (applicable extension rules)

| Rule | Status | How |
|---|---|---|
| security-baseline (supply-chain pin) | ✅ | `make subzy-db` pins commit; `--vuln_list` removes a runtime outbound; operator-provisioned |
| security-baseline (read-only rootfs) | ✅ | DB mounted read-only, not written at runtime; no data committed to repo/image |
| resiliency-baseline | ✅ (light) | absent-DB path stays `tool-failed`, never false-clean |
| property-based-testing | N/A | no algorithmic logic (asset + argv only) |

## Invariants
probe adapter interface · 26-item partition · CWE ownership · rate/gate/timeout envelope ·
non-root/read-only-rootfs hardening · subzy parser/schema (r9-validated, unchanged).
