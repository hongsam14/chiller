# Code Generation Summary — Unit `platform` (U1, last unit)

**Stage**: 🟢 CONSTRUCTION → `platform` → Code Generation (Part 2 complete) · **Date**: 2026-07-10
**Verification**: `go build ./...` · `go vet ./...` · `go test ./...` · `go test -race ./internal/platform/... ./test/` · `gofmt -l` (clean) · `make arch-check` (OK) — all green.

Platform is the **control plane + composition root**. It wires the finished data plane (scan/findings) end to end and creates no judgement logic. Every store/queue/progress seam is in-memory now but **Infra-swappable** (ctx-first, error-last, CAS `(bool,error)`, serializable entities), so Redis/Valkey/Mongo/OTel drop in at Infra without touching an interface or the orchestrator.

## What shipped (real + tested)

| Step | Package | Contents |
|---|---|---|
| 0 | `internal/domain` | `TransportKind` (`ProxyEligible` default / `DirectOnly`) + optional `TransportAware` capability + `TransportOf` helper (PR-12c). `probe` testssl declares `DirectOnly`; all others keep the default. |
| 1 | `internal/platform` | Contracts: `Scan` aggregate, `ScanState` FSM (+`Terminal()`), `ItemState`, `WorkLedgerEntry`, `ProgressSnapshot`; 7 seam interfaces (`TaskStore`, `WorkQueue`, `ProgressReporter`, `Authorizer`, `BrowserSession`, `AuthProxy`, `Orchestrator`); `ErrBadRequest`/`ErrNotFound`. |
| 2 | `…/taskstore` | In-memory `TaskStore` (map+RWMutex): scans, work ledger, CAS state/item, `ListUnfinished` (pending∪inflight), `AllItems`, `AllOutcomes`, reports. Contract tests: single-winner CAS, state-CAS guard, unfinished ledger, resume-no-reclaim. |
| 3 | `…/workqueue` | In-process FIFO with coalesced wakeup: ctx-blocking `Dequeue`, selective `Drain`. Tests: FIFO, block-until-enqueue, ctx-cancel-unblocks, drain drops a scan. |
| 4 | `…/progress` | In-memory counters; Done∪Failed partitions finished items; per-checker OK/Failed tally. |
| 5 | `…/api` | stdlib `net/http` ScanAPI (`POST /scans`, `GET /scans/{id}`, `GET /scans/{id}/report`, `GET /healthz`) + `BearerAuthorizer` (constant-time, fail-closed). Tests: authz fail-closed (no orchestrator call), 202 happy path, 400 bad/malformed, 202-while-running, 404, healthz-no-auth. |
| 6 | `…/orchestrator` | `Orchestrator` over injected seams + per-scan `exec.Executor`; **fan-out** (host-interleaved URL items + per-host items, PR-3a); **worker pool W** (CAS-claim → Run → RecordOutcome → progress, finalize-on-drain); **finalize** (dedup→store, **PR-5 fold**, `Accountant.Account`, `report.Build`, CAS running→completed); **resume** (re-enqueue unfinished, idempotent); **stop** (CAS→stopped, drain, ctx cancel); **BootGate** (26-item completeness or refuse). Tests: fan-out counts/interleave, PR-5 fold rules, boot pass + refuse. |
| 6b | `…/proxy` | `BrowserSession` (Playwright impl + `FakeSession`) with a **work tab pool T**; `AuthProxy` forward proxy (session injection + per-host rate); `LoginKeeper` seam. Tests: work cap == T with keep-alive reserved, session injection + forward, Serve ctx-cancel. |
| 6c | `scan/fetch`, `scan/probe` | `fetch.WithProxy` option (egress through the auth-proxy); probe transport declarations. Tests: fetcher egresses through proxy; testssl DirectOnly / rest ProxyEligible / all TransportAware. |
| 7 | `cmd/chiller` | Composition root: `--role=all\|manager\|worker\|proxy`, config load, **boot-gate-or-exit**, in-memory seam assembly, financial wiring, role dispatch. |
| 8 | `test/platform_e2e_test.go` | e2e submit→report **26-partition** + real finding; **fail-isolation** (ToolFailed isolates); **worker-pool concurrency** (4 delayed URLs < serial); **financial proxy** (target sees injected session, fake browser); resume idempotent; stop transitions. |

## Login keep-alive tab (user constraint)

A teammate's future login-agent package keeps the session alive on a **dedicated tab that must always be open**. The concurrency work tab pool `T` is therefore **work-only**; the keep-alive tab is reserved **separately and excluded from `T`** (total open pages = `T` + 1). `SessionConfig{Tabs, ReserveKeepAlive}` + the `LoginKeeper` seam model this; a test asserts `MaxConcurrency == T` even with `ReserveKeepAlive` set, so the keep-alive tab never steals a work slot.

## PR-5 fold (the one non-trivial reconstruction)

A `WorkItemOutcome` lists `Failed` and `NotApplicable` per checker but not which checkers ran *ok* (a finding carries `SourceTool`, not `CheckerID`). The fold recovers this from the orchestrator's own scope split: for each item it knows the checker set that ran (`urlScoped`/`hostScoped`), so `ran − failed − notApplicable = applicable-ok`. Rule: **applicable-ok anywhere → RanOK (Checked)**; **N/A everywhere, never applicable-ok → NotApplicable** (CWEs projected to PS-IDs via the mapper); **only ever failed → ToolFailed** (absence of RanOK). Coverage is Validate-clean by construction.

## Design refinements during code gen (logged in audit.md)

- `ProgressReporter.OnClaimed(item)` takes the `WorkItem` (not just its id) so inflight is attributed to the item's scan without a wid→scan index — Infra-robust.
- `TaskStore.AllItems` added to the seam so finalize can read each item's scope for the fold.
- Both are interface refinements, not new judgement logic; the FD's intent is unchanged.

## Honest scope (deferred to Build/Infra, same interfaces)

- **Backends**: Redis/Valkey `WorkQueue`, Mongo `TaskStore`, OTel exporter on `ProgressReporter`, OIDC/mTLS `Authorizer` — contract-tested now against the interface.
- **Browser transport**: Chromium provisioning + `playwright install`, HTTPS **CONNECT + MITM CA** issuance/trust, streaming/WS hardening, session auto-refresh. MVP `AuthProxy` forwards plain-HTTP absolute-form; the `BrowserSession`/injection path and single-session invariant are real (proven with `FakeSession`).
- **Probe env injection**: ✅ **wired 2026-07-10** — `probe.ConfigureProxy(adapters, proxyURL)` injects `HTTP(S)_PROXY` into every `ProxyEligible` adapter's subprocess env and leaves `DirectOnly` (testssl) direct, transport-driven (PR-12c). The `cmd/chiller` financial branch calls it. The exec seam now carries `env`; `defaultExec` applies it via `cmd.Env`. Tests: `TestConfigureProxy_TransportDriven`, `TestConfigureProxy_EnvReachesExec`. (The env only takes effect when a real bundled binary runs — the adapters are still stubs — but the routing mechanism is real and tested.)
- **New dependency**: `github.com/playwright-community/playwright-go v0.5200.1`.

## Roles (Q3=B, 4 roles)

`all` (standalone/dev: API + worker pool [+ in-process auth-proxy if `--financial`]) · `manager` (API + FSM; workers elsewhere, needs the Redis queue) · `worker` (worker pool; shared durable queue) · `proxy` (the single authenticated `BrowserSession` + `AuthProxy`; all workers egress through it → single-session invariant while scaling horizontally).
