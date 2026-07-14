<div align="center">

<img src="docs/logo.png" alt="chiller" width="200">

# chiller

**A passive web security scanner that fetches once, judges read-only, and can account for every item on its checklist.**

[![status](https://img.shields.io/badge/status-under%20development-orange)](#status--under-active-development)
[![not production ready](https://img.shields.io/badge/production-not%20ready-red)](#status--under-active-development)

</div>

---

## Status — under active development

**chiller is not finished and must not be pointed at anything you care about being right.** The skeleton is real — FSM, leases, durable backends, encryption, coverage accounting, CI — but **the detection engine is half-built**, and the gap is not cosmetic:

> **chiller judges 10 of the 26 KISA items. The other 12 are owned by probes that have never executed a single byte.**

Worse, the current build **does not say so**. All 7 external probe adapters are stubs whose parser returns `(nil, nil)` — "ran, found nothing" — which the coverage fold reads as **clean**. A scan of a trivial local page today reports:

```
26 PS items:  18 checked · 4 not_applicable · 4 delegated · 0 tool-failed
```

Eighteen items marked `checked` on a scan where twelve of them were never assessed at all. **That is a silent false negative, not a passing scan.** A fix that makes unwired probes fail loudly (`probe.ErrNotWired` → `tool-failed`) is written but sits on an unmerged branch; wiring it is item **#0** below.

Four of the ten *native* signatures have the same shape of problem from the other direction: PS-008 (`robots.txt`), PS-011 (`.env`), PS-012 (backup files) and PS-022 (`crossdomain.xml`) have real, tight regexes — but the executor only fetches the submitted URL, so the artifact they judge is **never requested**. They report clean because they were handed nothing to look at.

**Read the current output as a coverage skeleton, never as a security verdict.**

---

`chiller` takes a list of URLs and an explicit list of approved hosts, fetches each page exactly once with a plain `GET`, runs a set of read-only checkers over the response, and returns a report on two axes:

- **Findings**, keyed by **CWE** — what is wrong.
- **Coverage**, keyed by **PS-ID** against the 26-item **KISA** checklist — what was looked at, and what was not.

The two axes are kept structurally separate. Coverage is derived from *which checkers ran*, not by projecting the CWEs of the findings that happened to fire — so that a scan which finds nothing can still say what it actually looked at. Adding a new framework mapping changes the finding labels and leaves coverage bit-identical.

That is the design. Whether the *current build* lives up to it is a different question, and the honest answer is no — see [Status](#status--under-active-development) before you trust a report.

The service **refuses to start** unless every one of the 26 checklist items is accounted for by a checker or an explicit delegation. A scanner that cannot say what it covers should not be allowed to accept a scan. (It can account for all 26 today; it can only *judge* 10 — see [Status](#status--under-active-development).)

## Passive means passive

Every outbound byte passes a fail-closed gate. The scope control is an explicit **approved-host set** per scan, optionally intersected with a global allowlist — default deny, no exceptions.

- **`GET` only.** No payload injection, no fuzzing, no writes. Findings come from *observing* a normal response: headers, cookies, body, HTML comments.
- **Redirects are re-gated on every hop**, and re-charged against the rate budget. The initial check only covers the first URL.
- **JS discovery parses `<script src>` without executing JavaScript**, drops anything off-allowlist, and caps at 100 assets per page.
- **Per-host token bucket**, default 5 req/s. Retries spend a token too, so retries cannot outrun the budget.
- **Bounded**: 512 KiB max body, 20 s per request, 500 URLs per scan.

> [!IMPORTANT]
> There is **no SSRF or private-network guard**. The allowlist is the only scope control. If you approve `169.254.169.254`, chiller will fetch it. Approve hosts you own.

## Quick start

```bash
make dev-env                              # generates deploy/compose/.env with fresh random secrets
docker compose -f deploy/compose/docker-compose.yml up --build
```

Or run the whole thing in-process with no backends at all:

```bash
go run ./cmd/chiller --role=all --token=dev-token
```

Submit a scan:

```bash
curl -X POST localhost:8080/scans \
  -H 'Authorization: Bearer <YOUR_TOKEN>' \
  -d '{"urls":["https://example.com"],"approved":["example.com"]}'
# => 202 {"scanId":"scan-a1b2c3d4"}

curl localhost:8080/scans/scan-a1b2c3d4/report -H 'Authorization: Bearer <YOUR_TOKEN>'
```

A read-only ops dashboard is served at `/ui`.

## API

| Route | Auth | |
|---|---|---|
| `POST /scans` | ✅ | Create **and start** a scan. `202 {"scanId"}` |
| `GET /scans/{id}` | ✅ | State (`created`\|`running`\|`completed`\|`stopped`\|`failed`), progress counters, per-checker ok/failed |
| `GET /scans/{id}/report` | ✅ | The report, or `202 {"status":"running"}` if not ready |
| `GET /api/introspect/{queue,scans,persistence}` | ✅ | Ops introspection behind `/ui` |
| `GET /healthz` | — | Deep health: probes every wired backend |
| `GET /livez` | — | Shallow liveness |

Auth is a single static bearer token (`--token`), constant-time compared, **fail-closed** — an empty token denies every request and the API refuses to start without one. `platform.Authorizer` is a seam; swap it for OIDC or mTLS.

Request body:

```json
{
  "urls": ["https://example.com/login"],
  "approved": ["example.com"],
  "options": { "ratePerHost": 5, "burst": 5, "maxURLs": 500 }
}
```

## Architecture

```
                      ┌─────────────────────────────────────────┐
   POST /scans ──────▶│  platform  (control plane)              │
                      │  api · orchestrator FSM · work ledger   │
                      │  queue · leases · progress              │
                      └────────────┬────────────────────────────┘
                                   │ schedules, folds, records
                                   │ (creates no judgement)
                      ┌────────────▼───────────┐  ┌──────────────┐
                      │  scan  (data plane)    │  │   findings   │
                      │  ingest · safety gate  │─▶│  dedup       │
                      │  fetch · exec          │  │  coverage    │
                      │  checking/{obs,probe}  │  │  report      │
                      └────────────┬───────────┘  └──────┬───────┘
                                   │                     │
                      ┌────────────▼─────────────────────▼───────┐
                      │  domain  (shared kernel — no I/O)        │
                      │  Finding · CWE · Coverage · WorkItem     │
                      └──────────────────────────────────────────┘
```

Two layering rules, enforced by `make arch-check` (plain `go list -deps`, needs no linter) *and* by `depguard` in `.golangci.yml` — deliberately redundant, so the rule survives either tool going missing:

1. **The data plane must not import the control plane.** `scan` and `findings` never see `platform`.
2. **The domain kernel imports no unit.**

The control plane schedules; the data plane decides. `cmd/chiller/main.go` is the composition root — the only place that picks concrete implementations.

### The pipeline

**Boot gate** → load the mapping + coverage spec, build the checker set, and refuse to start if any of the 26 PS items is neither owned by a checker nor explicitly delegated.

**Accept** → validate and ingest the URL set (fail-closed on any malformed URL), mint a scan id, return `202` immediately.

**Fan out** → one URL-scoped work item per URL (all URL-scoped checkers share **one fetch**), plus one host-scoped item per distinct host. Items are **host-interleaved**, so one rate-limited host cannot head-of-line-block the queue.

**Work** → a pool of workers (default 16) claims items under a **60 s lease** with a 20 s heartbeat. A claim wins if the item is pending *or* inflight with an expired lease, so a dead worker's item gets stolen rather than lost. Outcomes are owner-guarded: a worker that lost its lease cannot double-count.

**Execute** → fetch → discover `<script src>` → fetch approved JS → run every checker over the shared response. A checker that errors is recorded as tool-failed and does not abort the others.

**Finalize** → dedup by identity key (max severity, unioned evidence) → persist findings → fold outcomes into coverage → build the report → **only then** flip the state to `completed`, so anyone who reads `completed` can always load the report. Evidence in the ledger is purged once the findings are durably stored; the response cache is wiped on every terminal path.

## Storage seams

Everything defaults to **in-memory**. Durable backends are opt-in, and a *partial* opt-in is a hard startup failure — the process will not quietly run with less durability or less encryption than you asked for.

| Seam | Default | Durable | Flag |
|---|---|---|---|
| Task store (scans, ledger, leases) | memory | **Mongo** — CAS via `findOneAndUpdate`, majority write concern | `--store` |
| Work queue | memory | **Valkey Stream** + consumer group, `XAUTOCLAIM` reaper | `--queue` |
| Findings store | memory | **Mongo**, one doc per finding, optional TTL | `--findings-store` |
| Response cache | memory | **Valkey** (a *separate* instance), 6 h TTL | `--cache` |

The cache Valkey must not be the queue Valkey — the process refuses to start if they match, because the cache's LRU policy would evict queued work items.

Everything sensitive written to a durable backend — finding evidence, ledger outcomes, whole cached responses — is sealed with **AES-256-GCM**, AAD-bound to its context so a sealed value cannot be transplanted between scans. Set `CHILLER_EVIDENCE_KEY` (`openssl rand -base64 32`); rotate with `"k2:<new>,k1:<old>"`. The repo ships no key material, not even a throwaway one.

## Roles

One binary, `--role` picks the mode.

| | |
|---|---|
| `all` *(default)* | API + in-process workers. Resumes orphaned scans at boot. |
| `manager` | API + FSM only; a durable queue feeds workers elsewhere. |
| `worker` | Workers only, no API. |
| `proxy` | The singleton Playwright auth-proxy (financial mode) — one pre-authenticated browser session that all workers egress through. |

Every scan goes through the HTTP API; there is no one-shot CLI scan mode.

## Configuration

| Env | |
|---|---|
| `CHILLER_MONGO_URI` | required for `--store=mongo` / `--findings-store=mongo` |
| `CHILLER_VALKEY_ADDR` / `_PASSWORD` | queue |
| `CHILLER_VALKEY_CACHE_ADDR` / `_PASSWORD` | cache (must differ from the queue) |
| `CHILLER_EVIDENCE_KEY` | **required if any durable backend is on** — 32 bytes, base64 |

The API token comes from `--token`.

| Config file | |
|---|---|
| `configs/coverage/kisa.yaml` | the 26-item accounting set + delegations. **The customer-changeable "what do we account for" surface.** |
| `configs/mappings/kisa.yaml` | CWE → framework projection, plus the attacker axis (ATT&CK) for items with no canonical CWE. Versioned; surfaces as the report's `MappingRevision`. |

## Development

```bash
make ci               # every blocking gate, exactly what CI runs
```

| | |
|---|---|
| `make test` | `go test -race ./...` |
| `make test-integration` | contract suites against real Mongo + Valkey containers |
| `make arch-check` | the two layering rules |
| `make lint` / `gosec` / `secrets` / `vuln` / `sbom` | pinned via `go run` and digest-pinned images — nothing to pre-install |
| `make cover` | reported, never gated |

Every tool version is pinned; there is no `latest`. `.golangci.yml` once sat in this repo for weeks, referenced by `make lint`, and never ran — golangci-lint was not installed anywhere. A gate you cannot run is not a gate.

Coverage is reported and never gated, on the theory that a coverage threshold buys tests written to raise a number.

## Roadmap

Source of truth: `aidlc-docs/construction/infrastructure-design/infra-plan.md` §7 and `aidlc-docs/operations/production-readiness.md`. Ordered by leverage, highest first.

### Detection — the actual gap

**#0 · Wire the 7 probe CLIs** — *top priority; nothing else on this list changes the fact that chiller judges 10 of 26 items.*
Bundle version-pinned binaries (`humble`, `httpx`, `corsy`, `changeme`, `testssl.sh`, `dirhunt`, `subzy`), author a real `argv` + `Parser` per tool, and exercise the real `os/exec` path at least once — today `defaultExec` is never reached in any code path. Setting `argv` non-nil flips the adapter to wired automatically; the subprocess runner, timeout, and `HTTP_PROXY` injection are already real and tested behind an injected exec fn. **Land the `ErrNotWired` honesty fix first**, so an adapter that cannot execute fails loudly instead of reporting clean. *(`internal/scan/checking/probe/adapters.go`, `Dockerfile`)*

**#0b · Fetch the well-known paths** — *high.*
Add a host-scoped provider that GETs a pinned path set (`/robots.txt`, `/.env`, `/.git/config`, `/crossdomain.xml`, a small backup-name list, `/admin`) through the **already gated and rate-budgeted** fetcher, and routes each response to the owning signature. A fixed known-path set is not crawling. **Do not "fix" this by returning N/A** — the items *are* applicable to the host; N/A would be a different lie.

**#0c · Signature quality.** Verified against Go RE2, 27/27 — the engine is sound, the dictionaries are shallow:
- **Decode `br`/`zstd`/`deflate` before matching.** Go's default transport handles gzip only and we set no `Accept-Encoding`; today this works by luck. The moment a target replies Brotli (Cloudflare's default), every body regex sees compressed bytes and matches nothing — an invisible, global false negative.
- **The 512 KiB body cap silently truncates.** Anchors past the cut are missed in large bundles. Never report clean on a truncated body — record the truncation.
- **Dictionary breadth**: PS-009 misses `AKIA…`/`ghp_…`/`sk_live_…`/JWTs (~7 patterns vs 100+); PS-011 misses `JWT_SECRET`; PS-012 flags a legitimate `report.zip` as High. *(no ReDoS risk — RE2 is linear-time; do not add backtracking hardening)*

### Infrastructure

| | |
|---|---|
| ✅ | TaskStore → Mongo, WorkQueue → Valkey *(2026-07-11)* |
| ✅ | FindingsStore → Mongo, ResponseCache → Valkey, envelope encryption |
| ✅ | CI pipeline, digest-pinned images, Go 1.26.5 |
| ⬜ | Financial mode: HTTPS via CONNECT + bundled MITM CA, Chromium provisioning, storageState refresh, LoginKeeper integration |
| ⬜ | ProgressReporter → OTel; Authorizer → OIDC/mTLS *(both are seams already; swap beside the existing impl, never rewrite)* |
| ⬜ | `PlaywrightDiscoverer` for dynamically-injected JS the static `<script src>` parser misses |
| ⬜ | Wire `configs/severity.yaml` — the file, `findings/severity` and `findings/normalize` are built and tested but **not in the live path**; checkers set severities directly |
| ⬜ | No HTTP route for `StopScan` / `ResumeScan` — both implemented, neither reachable |

### Before this runs in production (blocking)

- **#0 above.** Shipping this as "a KISA passive scanner" while it is silent on 12 items would hand customers a report that lies by omission.
- Provision secrets out-of-band (Mongo URI, Valkey password, API token, storageState) — `deploy/k8s/secret.example.yaml` is a template only.
- Enforce TLS + auth on Mongo and Valkey; Mongo replica set (3-node, majority), Valkey AOF + `noeviction`.
- Replace the static bearer token with the OIDC/mTLS `Authorizer`.

## Deployment

`deploy/compose/` — local dev and manual integration. Not production.

`deploy/k8s/` — a **reference skeleton** for the deferred infra rollout: manager (1), workers (HPA 2→20), the singleton proxy, Mongo replica set, both Valkeys, and a deny-by-default NetworkPolicy. Images are `REPLACE_WITH_DIGEST` placeholders and the HPA metric is a stand-in for a KEDA `XLEN` trigger. Read it, don't apply it.

The image is distroless static, non-root, `CGO_ENABLED=0 -trimpath`, digest-pinned at both stages.
