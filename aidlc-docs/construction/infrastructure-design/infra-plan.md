# Infrastructure Plan — chiller (consolidated, forward-reference)

**Created**: 2026-07-10 · **Status**: planning record for future work (nothing here is built yet).
**Scope**: everything the MVP deferred to "Build/Infra" across all 4 units, collected in one place so any item can be picked up independently.

---

## 0. Guiding principle — seam-swap, never rewrite

Every deferred backend has an **in-memory MVP impl already shipped + contract-tested**. Infra work adds a second impl **beside** it in the **same package**, behind the **same interface** — the orchestrator and every caller are untouched (E-6/PR-11).

- File convention: `taskstore.go` (mem) → add `taskstore_mongo.go`; `workqueue.go` → `workqueue_redis.go`; etc.
- Interface stays byte-identical (ctx-first, error-last, CAS `(bool,error)`, serializable entities). **If an Infra impl needs an interface change, that is a design smell — reconsider before editing the seam.**
- **Reuse the existing contract tests**: parameterize each `*_test.go` to run against both impls (table of constructors), so the Mongo/Redis impl must pass the exact same behavioral suite the mem impl passes.
- Selection lives ONLY in `cmd/chiller` via flags (`--store`, `--queue`, …). Today only `mem` is accepted; adding an impl = register it in the composition root, nothing else.
- Reference substrate: `aidlc-docs/inception/reverse-engineering/` (coolant) — Task FSM/CAS, asynq `TaskManager`, K8s `JobRunner`, Mongo, OTel — copied as patterns (Q5=A), AWS specifics stripped (cloud-agnostic).

---

## 1. Backend seam swaps (same interface, new impl beside mem)

| # | Seam (interface) | MVP impl (shipped) | Infra impl | File to add | Key notes | Acceptance |
|---|---|---|---|---|---|---|
| 1 | `platform.TaskStore` | `taskstore.Mem` (map+RWMutex) | **Mongo** | `internal/platform/taskstore/taskstore_mongo.go` | Scan aggregate + WorkLedger collections. `CompareAndSetState`/`CompareAndSetItemState` via `findOneAndUpdate` with the expected-state filter (atomic CAS). `ListUnfinished` = query `state ∈ {pending,inflight}`. Entities already serializable. | Passes the shared taskstore contract suite (CAS single-winner, unfinished ledger, resume-no-reclaim) against a real/mongo-test container. |
| 2 | `platform.WorkQueue` | `workqueue.Mem` (FIFO + coalesced wakeup) | **Redis/Valkey** | `internal/platform/workqueue/workqueue_redis.go` | `Dequeue(ctx)` = `BLPOP` (ctx→timeout loop); `Enqueue`=`RPUSH`; `Drain` = per-scan list/tag delete. At-least-once delivery is fine — the ledger CAS claim (PR-4) makes it exactly-once-effective. asynq-style (coolant `TaskManager`). | Contract suite (block-until-enqueue, ctx-cancel unblocks, drain drops a scan) green; **enables the real `manager`/`worker` role split** (see §6). |
| 3 | `platform.ProgressReporter` | `progress.Mem` (counters) | **+ OTel exporter** | `internal/platform/progress/progress_otel.go` (or wrap) | Keep the mem counters; emit OTel metrics/spans on Init/OnClaimed/OnFinished. The reporter is the ONLY telemetry emission point. | Snapshot behavior unchanged; metrics visible in a collector. |
| 4 | `platform.Authorizer` | `api.BearerAuthorizer` (static token) | **OIDC / mTLS** | `internal/platform/api/authorizer_oidc.go` | Validate JWT/OIDC or client-cert; still fail-closed. | Fail-closed tests pass with the new verifier; invalid/expired token → 401. |
| 5 | `fetch.ResponseCache` | `fetch.InMemory` | **Valkey** (volatile) | `internal/scan/fetch/cache_valkey.go` | TTL + wipe (FR-12/NFR-16), **encrypted-at-rest** for HAR bodies (SEC-01/03), `WipeScan` on scan end. Bounded body already enforced by the Fetcher. | Cross-checker fetch reuse (FR-02) holds across processes; bodies wiped post-scan. |
| 6 | `store.FindingsStore` | `store.InMemory` | **Mongo** | `internal/findings/store/store_mongo.go` | **Encrypted-at-rest evidence**, bounded snippets already enforced (`domain.MaxEvidenceSnippets`). | Save/Load round-trips; evidence never exceeds the bound; at-rest encryption verified. |

**Ordering within §1**: (1)+(2) first — they unlock durability + the manager/worker split and are the highest-value. (6)+(5) next (persistence + cache). (3)+(4) are additive/ops-facing.

---

## 2. Financial auth-proxy hardening (PR-12)

MVP shipped: `BrowserSession` (Playwright impl + `FakeSession`), `AuthProxy` (plain-HTTP absolute-form forwarding), tab pool T with the **login keep-alive tab excluded** (`ReserveKeepAlive` + `LoginKeeper` seam), single-session invariant, transport-driven routing (probes' `HTTP_PROXY` env + native fetch `WithProxy`, testssl `DirectOnly`).

Deferred to Infra/Build:
- **Chromium provisioning** + `playwright install` in the image/build (like the OSS-CLI bundle, §4).
- **HTTPS via CONNECT + bundled MITM CA**: issue a CA, distribute + trust it on workers, intercept TLS. `authProxy.handle` currently rejects non-absolute-form; add CONNECT tunneling + MITM. **Keep both HTTPClient paths gated** (the existing `HTTPClient2` discipline).
- **Interception hardening**: streaming responses, WebSocket upgrade, large bodies.
- **storageState lifecycle**: secret handling of the pre-provided authenticated state; **session auto-refresh** / token rotation.
- **LoginKeeper integration**: the teammate's login-agent package implements `proxy.LoginKeeper`, injected via `proxy.WithLoginKeeper`; it drives the reserved keep-alive tab. Interface + reserved slot already exist — **no work-pool change needed** (T stays work-only).

Acceptance: an HTTPS financial scan routes every worker through the one authenticated session; testssl still reaches the origin directly; keep-alive tab never reduces work concurrency (already unit-proven with the fake).

---

## 3. Probe CLI binaries + real parsers (SECURITY-10, C-5) — **TOP PRIORITY (§7 #1)**

**Status (2026-07-13): no OSS tool has ever been executed as a subprocess.** All 7 probe
adapters are unwired (`argv == nil`), no parser is authored (`stubParse` → `nil, nil`), and the
Dockerfile installs none of the binaries. The runner's `defaultExec` (real `exec.CommandContext`)
is therefore never reached in any code path, and has no test coverage.

**Consequence — the 12 items these probes own are NOT judged.** They are reported
`StatusToolFailed` ("chiller obtained no verdict"), which is honest but means only **10 of the
26 KISA items** are actually judged today. (Before 2026-07-13 an unwired probe returned
`(empty, nil)` and its items were reported **`StatusChecked`** — i.e. *clean* — turning every
weakness they own into a silent false negative. Fixed: `probe.ErrNotWired`, guarded by
`TestE2E_UnwiredProbes_ReportedAsToolFailed_NotChecked`. **Do not regress this**: an adapter that
cannot execute must fail, never return an empty clean result.)

Infra/Build work:
- **Bundle version-pinned binaries**: humble, httpx, testssl.sh, dirhunt, changeme, Corsy, subzy (`version: "pinned@build"` is a placeholder today; the Dockerfile has no install steps).
- **Author real `argv` + `Parser`** per tool (tool stdout → `domain.Finding`). The runner framework (subprocess exec + timeout + `HTTP_PROXY` env injection) is **already real and unit-tested with an injected exec fn** — only `argv`/`parse`/the binary are missing. Setting `argv` non-nil automatically flips `Adapter.Wired()` and stops the ErrNotWired failure.
- `probe.ConfigureProxy` env injection (done) takes effect automatically once `argv` is non-nil.
- **Exercise `defaultExec` at least once for real** (e.g. an integration test against a pinned binary) — the actual `os/exec` path is currently unproven.

Acceptance: each adapter parses its real tool output into findings with the correct CWE(s);
coverage still partitions 26 items; **the count of `tool-failed` items drops by that probe's
owned items and `checked` rises** (assert in `TestE2E_UnwiredProbes_…`); testssl runs direct,
others via proxy in financial mode.

---

## 4. Dynamic JS asset discovery (Post-MVP, r5/A-6/HC1)

MVP: `fetch.StaticDiscoverer` (parses `<script src>`, capped fan-out). Deferred:
- **`PlaywrightDiscoverer`**: load target in Chromium, capture requested JS URLs via net-capture. Same `fetch.Discoverer` seam → drop-in.
- File: `internal/scan/fetch/discover_playwright.go`. Gate + approved-host filtering identical to Static.

Acceptance: discovers dynamically-injected first-party JS the static parser misses; approved-host-only preserved; fan-out cap preserved.

---

## 5. Config / secrets provisioning
- storageState file, authz token(s)/OIDC config, per-scan allowlist, tool version pins, MITM CA — sourced from the deploy env (not committed). Document required env/secret mounts. `--store`/`--queue`/`--storage-state`/`--token` flags already exist.

---

## 6. Topology, deployment, NFR (from Requirements round 2 — deferred to NFR/Infra design)
- **manager/worker split**: real only with the durable shared queue (§1 #2, Redis). Today `all` is the working path; `worker`-only with the mem queue is a no-op (documented). After Redis: manager = API+FSM, workers = pull from Redis.
- **Deploy strategy reconciliation** (open questions, `aidlc-state.md`): Warm Standby (Q2r2=C) vs no-multi-region (Q7r2=X); canary (Q5r2=C) vs blue/green (Q6r2=C) → resolve in **NFR/Infra Design**.
- **CI/CD + AWS-dependency removal** (Q4r2=B), **change mgmt** (Q3r2=B), **IR + COE** (Q8r2=B).
- **NFR numeric bounds (A-4)**: throughput / p95 duration SLOs — set here with the chosen fleet + backends. Perf harness + scaling-curve method already documented in `build-and-test/performance-test-instructions.md`.
- **K8s JobRunner** pattern (coolant reference) if worker scaling moves to jobs.

---

## 7. Suggested execution order (highest leverage first)

> **#1 is not optional and not merely "nice fidelity". Until the probes are wired,
> chiller judges only 10 of the 26 KISA items.** The other 12 are reported
> `tool-failed` (no verdict) — correct and honest as of 2026-07-13, but it means
> the scanner is *half a scanner*. Nothing else on this list changes that.

1. **Probe binaries + real parsers (§3)** — **TOP PRIORITY.** Turns 12 of the 26 items from
   "no verdict" into actual judgements. This is the single biggest gap between chiller and
   its stated mission. Everything below is infrastructure around an engine that is still
   only judging 10 items.
2. **WorkQueue→Redis** + **TaskStore→Mongo** (§1 #1–2) — ✅ **DONE** (2026-07-11).
3. **FindingsStore→Mongo** + **ResponseCache→Valkey** (§1 #6, #5) — persistence + cross-process cache, at-rest encryption.
4. **Financial HTTPS/MITM hardening + LoginKeeper integration** (§2) — required for real financial-sector runs.
5. **OTel + OIDC/mTLS** (§1 #3–4) — observability + prod auth.
6. **PlaywrightDiscoverer** (§4), **deploy/NFR reconciliation** (§6).

## 8. Already done (do NOT redo)
- Native scan Fetcher resilience via `retryablehttp-go` (retries charge the per-host budget; redirect re-gate on both client paths; 5xx not retried). See `construction/scan/code/business-logic-summary.md`.
- Transport-driven probe `HTTP_PROXY` env injection wiring (`probe.ConfigureProxy`) + native fetch `WithProxy`. Mechanism real+tested; effective once real binaries land.
- All 4 units + Build & Test green; Operations (placeholder) reached.

## 9. Cross-references
- Seam interfaces + Infra-swap table: `construction/platform/functional-design/domain-entities.md` (E-5/E-6).
- Rules: `construction/platform/functional-design/business-rules.md` (PR-11 Infra-swap, PR-12 auth-proxy).
- Financial transport decisions: `construction/platform/functional-design/browser-proxy-transport-clarification.md`.
- Deferred-scope summaries: `construction/platform/code/business-logic-summary.md` §"Honest scope", `construction/build-and-test/build-and-test-summary.md`.
- Open design questions: `aidlc-state.md` §"Open design questions" + §"Backlog".
