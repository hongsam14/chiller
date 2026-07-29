# Build and Test Summary — subzy fingerprints DB provisioning (#0e, r18)

**Increment**: give subzy (PS-025 subdomain takeover) its takeover-fingerprint DB so it stops
self-fetching from a dead URL and can render a real verdict instead of `tool-failed`. Operator-provisioned
(`make subzy-db` + read-only volume + `HOME` override — subzy v1.1.0 has no fingerprint-path flag).
Single unit = `scan` (probe). Includes the workflow code-review fix pass (8 findings, 6 fixed).

## What changed (test-relevant)
- **`make subzy-db`** — pinned `curl` (commit `5bd4e12`, EdOverflow/can-i-take-over-xyz) → git-ignored
  `deploy/subzy/fingerprints.json`. Not committed; not baked into the image.
- **`subzyOutArg`** — no fingerprint flag (subzy v1.1.0 has none, verified via `run --help`).
- **`SUBZY_DB`** (default `/opt/subzy/fingerprints.json`) → `subzyHome()`/`subzyReadFile()`; the subzy
  adapter runs subzy with `HOME=subzyHome()` (via `runner.extraEnv`) so it reads the mounted DB.
- **`subzyDBGuard`** (`runner.precheck`) — fails the adapter (→ `tool-failed`) unless the file subzy
  will read is a present, non-directory, **non-empty JSON fingerprint array** — WITHOUT running subzy.
  Honesty independent of the tool: an absent/empty/malformed DB can never become a false-clean.
- Dockerfile `ENV SUBZY_DB`; `deploy/compose` + `deploy/k8s` read-only volume (k8s ConfigMap
  `optional: true`); `.gitignore`/`.dockerignore` exclude `deploy/`; deploy README operator note.

## Local gates — ALL GREEN (2026-07-29)
| Gate | Command | Result |
|---|---|---|
| Build | `go build ./...` | OK |
| Vet | `go vet ./...` / `-tags integration` | OK |
| Format | `gofmt -l internal/ cmd/ tools/` | clean |
| Architecture | `make arch-check` | OK |
| Unit + integration (race) | `go test -race ./...` | 23 pkg **ok**, 0 FAIL |
| SAST | `make gosec` | 0 findings (guard's ReadFile `#nosec G304`-justified) |
| Secret scan | `make secrets` | no leaks (deploy/subzy git-ignored, never committed) |
| Signature drift | `make signatures-check` | drift 0 |

## In-image Docker verification (the load-bearing gate, FR-S4)
Built `chiller:r18` (the Dockerfile ENV + `.dockerignore` changes build clean; subzy at
`/usr/local/bin/subzy`, `SUBZY_DB=/opt/subzy/fingerprints.json`). Ran the compiled probe integration
binary in the image:

- **(A) DB mounted read-only at `/opt/subzy/fingerprints.json`** → `HostProbes/subzy` **PASS**: the
  adapter runs subzy with `HOME=/opt`, subzy reads the mounted DB (no self-fetch/parse error) and
  produces a verdict. Proves the whole wiring (extraEnv HOME + guard + mount path) end-to-end in the image.
- **(B) no DB mounted** → `HostProbes/subzy` **SKIP** with `subzy fingerprint DB not provisioned …` — the
  guard fires (in production this is `tool-failed`, never a false clean): honest graceful degradation.
- **(C) `--read-only` rootfs + `--tmpfs /scratch` + DB mounted** → **PASS**: subzy runs with `HOME=/opt`
  on a read-only filesystem (review F5 settled — subzy does not write `$HOME` when the DB is present).

The full positive assertion against the takeover-mock fixture (`takeover-mock` → PS-025 finding) runs in
the opt-in `probe-fixture-positive` CI job, now provisioning the DB via `make subzy-db` + the `/opt/subzy`
mount.

## Code-review pass (high, 8 findings)
6 fixed, 1 verified-no-change (F5, read-only rootfs), 1 skipped (cross-file path literal). The most
severe (F1) was a real false-clean bug: the guard checked the literal `SUBZY_DB` while subzy read the
HOME-derived path — fixed so the guard checks the exact file subzy opens. golden 철칙: no violation.

## Invariants preserved
probe adapter interface · subzy parser (r9, unchanged) · 26-item partition · CWE ownership ·
rate/gate/timeout envelope · non-root/read-only-rootfs hardening (DB mounted read-only). No data committed.

## Carried forward (honest)
- Auto-refresh of the fingerprint DB (CI cron + drift) — Q3 chose manual; a documented follow-up.
- The positive fixture assertion is opt-in CI (`workflow_dispatch`), not a blocking gate (r7 IQ2=A).
