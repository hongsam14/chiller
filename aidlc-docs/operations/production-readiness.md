# Production Readiness — chiller (infrastructure increment)

**Stage**: OPERATIONS (placeholder in this AI-DLC workflow — no deployment build is executed here).
**Date**: 2026-07-11. Captures the handoff checklist for the platform U1 seam-swap increment (Mongo + Valkey backends, introspection UI, deploy assets).

The code is **green** (build/vet/gofmt/arch-check/`go test ./...`/`-race`). Below is what MUST happen before the durable backends run in production, and the honest limitations carried forward.

## Mandatory before production (blocking)

> **#0 — MOSTLY CLOSED 2026-07-14 (probe-wiring increment r7), one Docker gate remains.**
> Six of the seven probes are now wired (argv + real JSON parser + bundled binary): humble,
> httpx, Corsy, testssl.sh, dirhunt, subzy. Only **changeme (PS-019) stays unwired** — Python2
> heritage + active credential-testing + uncertain JSON schema (F5), honestly reported
> `tool-failed`, deferred to a later increment. So on a real run chiller now judges up to
> **21 of 26** items (10 observation + 11 probe-owned; changeme's 1 + the 4 delegated remain).
> **Remaining gate below (#0a): the wiring is verified locally by unit + honesty tests, but the
> real binaries have never run — the in-image `probe-integration` job must pass once on Docker.**

| # | Action | Why | Where |
|---|---|---|---|
| ~~0~~ | ~~Wire the 7 probe CLIs~~ **DONE (6/7) 2026-07-14** — argv + JSON parser + golden fixtures per tool; multi-stage image bundles the pinned binaries; process-group kill (NFR-P1) added. changeme deferred (F5). | 12 items were `tool-failed`; now up to 11 move to judged once binaries run. Honesty invariant preserved (wired ≠ binary-present → still tool-failed until the binary runs). | `internal/scan/checking/probe/{adapters,parsers,exec_linux}.go`, `Dockerfile`; summary `construction/scan/code/probe-business-logic-summary.md` |
| ~~0a~~ | **Run the in-image `probe-integration` on Docker** — **DONE 2026-07-14.** Built the image and ran all 6 wired probes with real binaries in-image: **6/6 execute + parse clean** (URL humble/httpx/corsy; Host testssl/dirhunt/subzy vs a real host). The first run corrected the hand-authored I/O model (file-output vs stdout), tool packaging, deps (hexdump/dig), and timeouts — see `construction/scan/code/probe-business-logic-summary.md` §Docker validation. | Exec+run+parse-doesn't-break is now proven. **Still open below (#0d):** positive-*finding* extraction is unverified (benign targets return empty/clean); humble's parser is known to need a rewrite against its real section-based schema. | `.github/workflows/ci.yml` `probe-integration` |
| 0b | ~~Confirm tool version pins~~ **DONE 2026-07-14** (at first build): `httpx@v1.6.10`, `subzy@v1.1.0` (module path `LukaSikic/subzy`), testssl `v3.2.1`, `dirhunt==0.9.0`; humble & Corsy are **git clones** (no PyPI release), not pip. **Still: pin the `python:3.12-slim-bookworm` base and the git tools by digest/commit** (SECURITY-10/13). | Versions now verified to build & run; digest/commit pin remains for supply-chain integrity. | `Dockerfile`; Dependabot |
| ~~0c~~ | **subzy writable HOME under read-only rootfs** — **DONE 2026-07-14**: `ENV HOME=/scratch` in the image + `fsGroup: 10001` on the worker pod so the `/scratch` emptyDir is writable by the non-root uid. Validated with `docker run --read-only --tmpfs /scratch` (subzy PASS). | subzy caches its fingerprints DB in `$HOME`; read-only rootfs made `/home/app` unwritable. | `Dockerfile`, `deploy/k8s/worker.yaml` |
| 0d (partial) | **Validate & fix positive-finding parser schemas** — **DONE for humble/httpx/Corsy 2026-07-15** (increment r8, against OWASP Juice Shop + versioned nginx): humble rewritten to its real section-based schema (headers only → CWE-319/1021; **cookie PS-015/016/017 moved to the observation Set-Cookie engine** — humble never analysed cookies), Corsy rewritten to its real URL-map schema (→ CWE-346), httpx golden refreshed to real output. In-image integration now asserts POSITIVE findings (humble CWE-319, httpx CWE-693, Corsy CWE-346). **STILL OPEN: testssl (PS-002) / dirhunt (PS-004) / subzy (PS-025)** — Juice Shop local http has no weak-TLS / dir-listing / dangling-subdomain surface, so their positive-finding schemas remain unmeasured; validate when such targets are available. | `internal/scan/checking/probe/parsers.go` + `internal/scan/checking/observation/` (cookie sigs); summary `construction/scan/code/parser-fidelity-summary.md` |
| 1 | **Run the Docker-backed integration suite** — `make test-integration` in a CI job with a Docker daemon | The Mongo/Valkey contract suites compile but were **never executed** (no Docker in the authoring env). Until they pass against real containers, the durable backends are unverified. | `internal/platform/{taskstore,workqueue}/*_test.go` (`//go:build integration`) |
| 2 | **Bump the Go toolchain to ≥ 1.26.5 and rebuild** | `govulncheck` flagged **GO-2026-5856** (crypto/tls ECH privacy leak) on our call paths (proxy/fetch/Mongo/Valkey TLS). Fixed in go1.26.5. No source change needed. | build pipeline / Dockerfile base |
| 3 | **Replace every image tag with a digest** (`@sha256:…`); no `latest` | SECURITY-10/13 supply-chain integrity | `Dockerfile`, `deploy/k8s/*.yaml`, `deploy/compose/docker-compose.yml` |
| 4 | **Provision secrets out-of-band** (Mongo URI w/ `tls=true&w=majority`, Valkey addr+password, API token, storageState) via sealed-secrets/vault | SECURITY-12 — never commit | `deploy/k8s/secret.example.yaml` is a template only |
| 5 | **Enforce TLS + auth on Mongo and Valkey**; enable Mongo replica set (3-node, majority) and Valkey AOF+noeviction | SECURITY-01/09, Q6=B/Q12=B durability | backend config |
| 6 | **Wire CI**: `go vet` · `gofmt` · `make arch-check` · `go test ./...` · `make vuln` · `make test-integration` (Docker job) · `make sbom` · image build w/ digest | RESILIENCY-04, SECURITY-10 | new pipeline |

## Recommended (non-blocking)
- Set the worker HPA to a **KEDA ScaledObject on Valkey stream length** (the manifest ships a portable CPU-based placeholder).
- Add resource requests/limits, PodDisruptionBudgets, and topology-spread constraints per AZ to the k8s manifests.
- Configure Mongo automated backups (oplog + daily snapshot, 7-day retention, encrypted) + a quarterly test-restore (RESILIENCY-12).
- Alerting on: auth failures, `/healthz` degraded, queue depth (consumer-group lag) growth, reaper reclaim rate, Mongo replication lag (RESILIENCY-05/07/14).

## Known limitations carried forward (honest — from code review + design)
1. ~~Split-topology mid-run recovery is not automatic.~~ **RESOLVED 2026-07-11 (ledger-lease increment).** Items carry a lease (owner + expiry, heartbeat-refreshed); a lease-safe periodic sweep (manager + all, `ReclaimExpired`) reclaims only expired-lease inflight items, so a dead worker's items are recovered mid-run without clobbering a live worker's. The dashboard scan list (`/api/introspect/scans`) is also live. Safe-reclaim invariant (at most one recorded outcome per item) proven by a stateful PBT. See `construction/platform/code/lease-business-logic-summary.md`.
2. ~~Probe adapters are stubs (`argv` nil).~~ **RESOLVED 6/7 2026-07-14 (increment r7).** Six probes wired (argv + JSON parser + bundled binary + process-group kill); changeme (PS-019) deferred (F5). Remaining: the in-image `probe-integration` job must run once on Docker to validate the hand-authored parser schemas against real output (#0a) and confirm the tool version pins (#0b). See `construction/scan/code/probe-business-logic-summary.md`.
3. **Not in this increment** (infra-plan #3–#6): ProgressReporter→OTel, Authorizer→OIDC/mTLS, ResponseCache→Valkey, FindingsStore→Mongo. The shared-instance placement for #5/#6 is pre-decided in `construction/shared-infrastructure.md`.
4. **Browser transport hardening** (infra-plan §2): Chromium provisioning, HTTPS CONNECT + MITM CA, session auto-refresh.

## Suggested next tasks (pick one)
- **Ledger lease** → safe split-topology recovery + dashboard scan list (closes the #1 limitation).
- **infra-plan #6 FindingsStore→Mongo** / **#5 ResponseCache→Valkey** (reuse the shared-instance placement already decided).
- **Probe binaries + real parsers** (infra-plan §3) — biggest coverage-fidelity gain.
