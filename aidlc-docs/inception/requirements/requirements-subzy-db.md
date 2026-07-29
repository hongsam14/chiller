# Requirements — subzy fingerprints DB provisioning (#0e, r18)

**Increment**: give `subzy` (PS-025, subdomain takeover) its takeover-fingerprint DB so it
stops self-fetching from a dead URL and can render a real verdict instead of `tool-failed`.
The argv + parser were already validated against real output in r9 (#0d) — this closes the
runtime-provisioning gap left open then.

**Unit**: `scan` (probe) — a vendored asset + Dockerfile + a one-line argv change. No new
deployable, no interface change.

## Problem (grounded)

- `subzyOutArg` runs `subzy run --target <host> --output <file> --hide_fails` (parsers.go:117).
- subzy v1.1.0 loads its vulnerability fingerprints by **self-fetching** them on each run; that
  source URL is dead, so subzy receives a 404 HTML page, fails to parse it as JSON
  (`invalid character ':'`), and exits non-zero → the adapter honestly reports PS-025
  `tool-failed`. The Dockerfile even sets `HOME=/scratch` so subzy could cache the download
  (#0c) — but the download never succeeds.

## Decisions (Requirements Analysis, 2026-07-29)

- **Q1 (revised) — operator-provisioned via a `make` target + volume, NOT repo/image-vendored.**
  A `make subzy-db` target fetches `fingerprints.json` at a **pinned commit** to a git-ignored host
  path; the operator injects it into the container as a **read-only volume**. The DB is subzy's own
  operational data (community-maintained, changing), so it is treated like the project's other
  operator-provisioned material (secrets via `make dev-env`, storageState, `deploy/k8s/secret.example.yaml`
  template-only) — the repo ships the tooling + docs, not the data. The fetch is a one-time
  provision step, NOT a per-run runtime fetch, so a dead URL fails loudly at provision time (fixable)
  rather than silently at scan time (the #0e failure mode is avoided).
- **Q2 = B (revised at Code Gen).** subzy v1.1.0 has **no fingerprint-path flag** (`--vuln_list`
  does not exist — verified against `subzy run --help`). It loads `$HOME/subzy/fingerprints.json` and
  self-fetches only if absent. So the DB is mounted read-only at `$SUBZY_DB` and the adapter runs subzy
  with `HOME` set so that path resolves (verified in a container: read-only mount → clean run, no
  self-fetch). Still removes the outbound and fits the read-only rootfs.
- **Q3 = manual pin.** The `make subzy-db` target pins the commit/URL; refresh = re-run the target
  (operator responsibility). A CI auto-refresh + drift job is a later follow-up.

## Functional Requirements

- **FR-S1** — A `make subzy-db` target fetches `fingerprints.json` at a **pinned commit** to a
  git-ignored host path (e.g. `deploy/subzy/fingerprints.json`), with the source URL + commit recorded
  in the target/docs. It does NOT commit the DB to the repo. (Used both by the operator to provision
  and by CI to obtain the DB.)
- **FR-S2** — The container consumes the DB via a **read-only volume mount** at the `--vuln_list`
  path (e.g. `/opt/subzy/fingerprints.json`); `deploy/compose` and `deploy/k8s` document/wire the mount.
  The Dockerfile does NOT bake the DB.
- **FR-S3** — the DB is mounted at `$SUBZY_DB` (default `/opt/subzy/fingerprints.json`) and the subzy
  adapter runs subzy with `HOME` derived from it (so `$HOME/subzy/fingerprints.json` = the mount), a
  local/integration run overriding `SUBZY_DB` to the fetched host file. subzy no longer self-fetches.
  (subzy v1.1.0 has no fingerprint flag — see Q2.)
- **FR-S4** — Verified on a real Docker run: with the DB mounted, subzy executes without the
  self-fetch/parse error, and PS-025 yields a real verdict — positive against a takeover fixture,
  clean against a benign host (extends the r9 in-image probe validation).

## Non-Functional

- **NFR-S1 (honesty)** — if the DB is absent (a broken deploy), subzy falls back to its self-fetch
  and still fails → `tool-failed`, never false-clean. The image always bundles it, so this is only
  a deploy-integrity edge. No boot-gate change.
- **NFR-S2 (security/supply-chain)** — the DB is pinned by commit; `--vuln_list` REMOVES a runtime
  outbound (net-negative attack surface). New vendored data is scanned by `make secrets`/allowlisted
  as third_party.
- **Invariants** — probe adapter interface · 26-item partition · CWE ownership · rate/gate/timeout
  envelope · non-root/read-only-rootfs hardening (the DB is read-only, not written at runtime).

## Rationale — how these requirements were derived (evidence → reasoning → requirement)

- **Scope = provisioning only, not logic.** r9 (#0d) already validated subzy's `argv` + parser
  against real `--output` schema; the only open gap it recorded was runtime DB provisioning. So the
  problem reduces to "how is the DB supplied", not any code logic — hence parser/schema changes are
  explicitly out of scope.
- **FR-S3 (`--vuln_list`) — remove the self-fetch.** The dead-URL self-fetch is the direct cause of
  the failure (404 HTML → JSON parse error). chiller also minimizes probe outbound traffic to the
  trust boundary (approved ∩ allowlist ∩ rate). Making subzy read a LOCAL DB both removes the dead-URL
  dependency and removes a runtime outbound (net-negative attack surface). The exact flag is unverified
  → flagged as a Build & Test risk with a `$HOME/subzy` cache fallback.
- **FR-S2 (read-only image path) — hardening + immutability.** #0c established non-root + read-only
  rootfs + `/scratch` (emptyDir) + cap-drop. `/scratch` is writable but starts empty per container, so
  a cached DB there would need a per-run entrypoint copy. The DB is immutable, so baking it into a
  read-only image path is simpler and matches the hardening; `--vuln_list` is what makes a read-only
  reference possible, so FR-S2 and FR-S3 reinforce each other.
- **FR-S1/FR-S2 (operator-provisioned via make target + volume, pinned) — data, not baked.** subzy's
  fingerprints.json is subzy's OWN operational data (community-maintained, changing), not a detection
  rule we author — so it fits the project's established **operator-provisioning** pattern (`make
  dev-env` generates secrets, storageState + `deploy/k8s/secret.example.yaml` are template-only, the
  repo ships no key material). A `make subzy-db` target + a read-only volume mount keeps the DB
  operator-owned and refreshable without an image rebuild, and does NOT freeze community data into the
  repo/image. The fetch is **pinned by commit** and happens at **provision time (once), not per run** —
  so #0e's failure mode (a per-run fetch of a moving target failing silently at scan time) is avoided:
  a dead URL fails loudly at `make subzy-db`, fixable before deploy. Accepted trade: PS-025 needs the
  operator to provision the DB (consistent with every other operator-provided input); absent → honest
  `tool-failed`, never false-clean.
- **NFR-S1 (honest on absence).** The project-wide invariant is tool-failed ≠ false-clean (r7
  ErrNotWired spirit). If the DB is absent, subzy self-fetches, fails, and reports `tool-failed` — never
  a fabricated clean. Honesty is therefore already guaranteed without an extra boot gate.

## Out of scope (carried)

- Auto-refresh of the fingerprints DB (CI cron) — Q3 chose manual; a later follow-up.
- `changeme` (PS-019) wiring — a separate increment.
- Any change to subzy's parser/schema (validated in r9; unchanged here).

## Extension compliance (applicable)

| Extension | Applicable | How |
|---|---|---|
| security-baseline | Yes | pinned vendored DB; `--vuln_list` removes a runtime outbound; read-only asset |
| resiliency-baseline | Yes (light) | absent-DB path stays honest (`tool-failed`), never false-clean |
| property-based-testing | N/A | no new algorithmic logic (asset + argv only) |
