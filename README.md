<div align="center">

<img src="docs/logo.png" alt="chiller" width="200">

# chiller

**A passive web security scanner that fetches once, judges read-only, and can account for every item on its checklist.**

[![status](https://img.shields.io/badge/status-under%20development-orange)](#status--under-active-development)
[![not production ready](https://img.shields.io/badge/production-not%20ready-red)](#status--under-active-development)

</div>

---

## Status — under active development

**chiller runs end to end, but it is not production-hardened — do not point it at anything you care about being right yet.** The engine is now wired where earlier builds were hollow:

- **All checkers execute.** The six external probe CLIs — `humble`, `httpx`, `corsy`, `testssl.sh`, `dirhunt`, `subzy` — run for real through the subprocess path, each with a pinned `argv` and a parser verified against captured tool output. `changeme` (PS-019) is the one remaining stub, and it fails **loudly** as `tool-failed` — an adapter that cannot execute never reports a verdict it did not reach (`probe.ErrNotWired`).
- **Well-known paths are fetched.** A provider GETs a fixed, benign path set (`/robots.txt`, `/.env`, `/.git/config`, `/.DS_Store`, `/crossdomain.xml`, a backup-name list, `/admin`, API-doc surfaces, product-exposure surfaces like `/actuator/env` · `/phpinfo.php` · `/server-status`) through the **already gated and rate-budgeted** fetcher and routes each response to the signature that owns it — so the native checks judge an artifact they were actually handed.
- **Signatures are distilled from real OSS rulesets**, not hand-rolled: secrets from **gitleaks** (221 rules + Shannon-entropy gating + allowlists), sensitive URL params from **OWASP ZAP**, API-doc and product-exposure surfaces + framework stack-trace/error pages from **nuclei-templates**, internal/infra disclosure from **ZAP's Private-IP** rule — every pattern ported to Go RE2 at authoring time and pinned under `configs/signatures/`. Distilled, *not* executed at runtime: chiller depends on no external scanner engine.
- **Passive error/stack-trace signals, honestly labelled.** chiller carries nuclei-distilled markers + curated runtime frame regexes that surface a stack trace or verbose error *already present* in a fetched response — but **PS-005/006 stay delegated to Nuclei**, because honest end-to-end detection needs an *active* probe that elicits the error. So those passive hits are reported as informational findings (CWE-209/497) *outside* the KISA-26 accounting, never claiming to "check" PS-005/006. A backup finding now requires a real archive magic (`PK`/`SQLite`/SQL-dump), and a **personal-information exposure** check (Korean resident-id / email / phone, CWE-359, likewise reported *outside* the 26 with the value masked) rides alongside. Delegated set: **PS-005/006** (→ Nuclei), **PS-014** (→ ASM), **PS-026** (→ Nuclei).

The design invariant holds: **coverage is derived from which checkers ran, never from which findings fired**, so a checker that did not run — or tool-failed — is never counted as `checked`. A report is an honest statement of what was assessed.

Still open before this is a production KISA scanner:

- `changeme` (PS-019) is unwired; `subzy` needs its takeover-fingerprint DB provisioned at runtime or it tool-fails.
- Response bodies are **not decoded for `br`/`zstd`/`deflate`** before matching, and the **512 KiB body cap truncates silently** — both can cause false negatives (see [Roadmap](#roadmap)).
- Financial (authenticated) mode, the OTel/OIDC seams, and the `StopScan`/`ResumeScan` HTTP routes are implemented-or-seamed but not fully wired.

**Treat chiller as pre-1.0: the verdicts it does render are honest, but the coverage is not yet complete.**

---

`chiller` takes a list of URLs and an explicit list of approved hosts, fetches each page exactly once with a plain `GET`, runs a set of read-only checkers over the response, and returns a report on two axes:

- **Findings**, keyed by **CWE** — what is wrong.
- **Coverage**, keyed by **PS-ID** against the 26-item **KISA** checklist — what was looked at, and what was not.

The two axes are kept structurally separate. Coverage is derived from *which checkers ran*, not by projecting the CWEs of the findings that happened to fire — so that a scan which finds nothing can still say what it actually looked at. Adding a new framework mapping changes the finding labels and leaves coverage bit-identical.

That is the design, and the current build now holds to it — coverage stays honest even where detection is still incomplete. Read [Status](#status--under-active-development) for what is and isn't wired before you trust a report.

The service **refuses to start** unless every one of the 26 checklist items is accounted for by a checker or an explicit delegation. A scanner that cannot say what it covers should not be allowed to accept a scan. (Every item is now judged by a live checker or explicitly delegated; `changeme`/PS-019 is the lone unwired stub, and it fails loudly rather than reporting clean — see [Status](#status--under-active-development).)

## Passive means passive

Every outbound byte passes a fail-closed gate. The scope control is an explicit **approved-host set** per scan, optionally intersected with a global allowlist — default deny, no exceptions.

- **`GET` only.** No payload injection, no fuzzing, no writes. Findings come from *observing* a normal response: headers, cookies, body, HTML comments, and statically-discovered JS source.
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
| `GET /report` | — | Compliance report **view** — an HTML render of a scan's coverage against the selected framework, with per-finding drill-down |
| `GET /api/introspect/{queue,scans,persistence}` | ✅ | Ops introspection behind `/ui` |
| `GET /healthz` | — | Deep health: probes every wired backend |
| `GET /livez` | — | Shallow liveness |

Auth is a single static bearer token (`--token`), constant-time compared, **fail-closed** — an empty token denies every request and the API refuses to start without one. `platform.Authorizer` is a seam; swap it for OIDC or mTLS.

Request body:

```json
{
  "urls": ["https://example.com/login"],
  "approved": ["example.com"],
  "framework": "KISA",
  "options": { "ratePerHost": 5, "burst": 5, "maxURLs": 500 }
}
```

`framework` selects the accounting framework for this scan (`KISA` default, or `FSEC`); it changes the report's item set and labels while coverage stays computed from which checkers ran. Omit it for the server default.

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
| `configs/coverage/{kisa,fsec}.yaml` | the accounting item set + delegations, per framework. **The customer-changeable "what do we account for" surface.** |
| `configs/mappings/{kisa,fsec}.yaml` | CWE → framework projection, plus the attacker axis (ATT&CK) for items with no canonical CWE. Versioned; surfaces as the report's `MappingRevision`. |
| `configs/signatures/*.yaml` | the OSS-distilled observation rules (secrets, url-sensitive, api-docs, internal-disclosure, exposures, error-disclosure), loaded fail-closed at boot via `--signatures`. **Generated — do not hand-edit**; run `make signatures` to regenerate from the pinned `third_party/` sources. |
| `configs/severity.yaml` | the severity-normalization table (built and tested; see [Roadmap](#roadmap) for wiring status). |

The **default** framework is whichever `--coverage` / `--mapping` pair is loaded; **sibling** framework configs in the same directory are auto-discovered at boot and become selectable per scan via the request's `framework` field (see [API](#api)). A secondary framework that fails its own boot gate is skipped with a warning and never takes down the default.

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
| `make signatures` / `signatures-check` | regenerate `configs/signatures/` from the pinned `third_party/` OSS sources, and fail CI if the committed config drifts from a fresh regeneration |
| `make cover` | reported, never gated |

Every tool version is pinned; there is no `latest`. `.golangci.yml` once sat in this repo for weeks, referenced by `make lint`, and never ran — golangci-lint was not installed anywhere. A gate you cannot run is not a gate.

Coverage is reported and never gated, on the theory that a coverage threshold buys tests written to raise a number.

## Roadmap

Source of truth: `aidlc-docs/construction/infrastructure-design/infra-plan.md` §7 and `aidlc-docs/operations/production-readiness.md`. Ordered by leverage, highest first.

### Detection

| | |
|---|---|
| ✅ | **Probe CLIs wired** — `humble`, `httpx`, `corsy`, `testssl.sh`, `dirhunt`, `subzy` run through the real `os/exec` path, each with a pinned `argv` + a parser verified against captured output; the `ErrNotWired` honesty seam keeps an unwired adapter `tool-failed`, never clean. |
| ✅ | **Well-known-path provider** — a fixed benign path set is fetched through the gated, rate-budgeted fetcher and routed to the owning signature (not crawling; applicable items are never faked to N/A). |
| ✅ | **Signatures distilled from OSS** — gitleaks (221 secret rules + entropy gating), ZAP url-sensitive params, nuclei API-doc surfaces, ZAP Private-IP internal disclosure — ported to Go RE2, pinned in `configs/signatures/`, regenerated by `make signatures` and drift-checked in CI. |

**Remaining detection gaps:**

- **`changeme` (PS-019) still stubbed**, and **`subzy` needs its takeover-fingerprint DB** provisioned at runtime — without it, subzy tool-fails rather than reporting clean.
- **Decode `br`/`zstd`/`deflate` before matching.** Go's default transport handles gzip only and we set no `Accept-Encoding`; today this works by luck. The moment a target replies Brotli (Cloudflare's default), every body regex sees compressed bytes and matches nothing — an invisible, global false negative.
- **The 512 KiB body cap silently truncates.** Anchors past the cut are missed in large bundles. Never report clean on a truncated body — record the truncation. *(no ReDoS risk from the widened dictionaries — RE2 is linear-time; do not add backtracking hardening.)*

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

- **Close the detection gaps above** — wire `changeme`, provision the `subzy` DB, decode compressed bodies, and stop truncating silently. The coverage is honest, but it is not yet complete.
- Provision secrets out-of-band (Mongo URI, Valkey password, API token, storageState) — `deploy/k8s/secret.example.yaml` is a template only.
- Enforce TLS + auth on Mongo and Valkey; Mongo replica set (3-node, majority), Valkey AOF + `noeviction`.
- Replace the static bearer token with the OIDC/mTLS `Authorizer`.

## Deployment

`deploy/compose/` — local dev and manual integration. Not production.

`deploy/k8s/` — a **reference skeleton** for the deferred infra rollout: manager (1), workers (HPA 2→20), the singleton proxy, Mongo replica set, both Valkeys, and a deny-by-default NetworkPolicy. Images are `REPLACE_WITH_DIGEST` placeholders and the HPA metric is a stand-in for a KEDA `XLEN` trigger. Read it, don't apply it.

The image is distroless static, non-root, `CGO_ENABLED=0 -trimpath`, digest-pinned at both stages.
