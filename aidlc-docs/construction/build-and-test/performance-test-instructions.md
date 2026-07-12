# Performance Test Instructions — chiller

## Purpose
Validate that chiller's throughput scales with the worker pool while respecting the safety envelope (per-host rate budget, single-session invariant). chiller is a **passive** scanner: performance is a *constraint* subordinate to coverage completeness (C7=A) — going faster must never breach the per-host rate limit or the fail-closed gate.

## Performance Requirements (bounds deferred to NFR/Infra, A-4)
Numeric SLOs (throughput, p95 duration) were explicitly deferred to NFR Requirements / Infra (see `aidlc-state.md` open questions A-4). The properties below are what the MVP guarantees and how to measure them; concrete numbers are set when the target fleet + Infra backends (Redis/Mongo) are chosen.

- **Parallelism**: effective concurrency ≈ `min(W, per-host rate, T)` where W=`--workers`, T=`--browser-tabs` (financial mode only).
- **Per-host safety**: no host is hit faster than `RatePerHost` (burst-clamped) regardless of W.
- **Financial single-session**: N workers share exactly one authenticated `BrowserSession`; concurrency is T-bounded (login keep-alive tab excluded from T).

## Setup Performance Test Environment
### 1. Prepare Test Environment
```bash
go build -o bin/chiller ./cmd/chiller
# Stand up a controllable target (latency-injecting) — e.g. a local echo server with tunable delay.
```
### 2. Configure Test Parameters
- **Workers**: sweep `--workers` ∈ {1, 4, 16, 32}.
- **Rate**: `options.ratePerHost` ∈ {1, 5, 20} to observe the throughput ceiling per host.
- **Financial**: `--financial --browser-tabs` ∈ {1, 6, 12} to observe the T-bound.

## Run Performance Tests
### 1. Micro-benchmark the hot paths (in-repo)
```bash
# Add Go benchmarks (Benchmark*) for fan-out, fold, and dedup, then:
go test -bench=. -benchmem ./internal/platform/orchestrator/ ./internal/findings/dedup/
```
### 2. Worker-pool scaling (already asserted qualitatively)
```bash
go test -race -run TestE2E_WorkerPoolConcurrency -count=5 ./test/
# Confirms 4 delayed URLs run in parallel (<350ms vs ~480ms serial). Extend the harness to
# sweep W and record wall-clock vs. serial baseline for a scaling curve.
```
### 3. Rate-limit conformance (load)
```bash
# Drive many URLs on ONE host with ratePerHost=5; assert observed request rate at the target
# stays <= 5/s + burst regardless of --workers (the per-host token bucket is the ceiling).
```

### 4. Analyze Results
- **Throughput**: requests/sec should rise with W until the per-host rate (single host) or aggregate host count (multi-host) caps it.
- **Bottlenecks (by design)**: per-host token bucket (safety), and in financial mode the tab pool T.
- **Error Rate**: transient failures currently become `ToolFailed` (no retry) — the **retryablehttp-go backlog item** targets this; re-measure ToolFailed rate before/after adoption.
- **Results Location**: `go test -bench` output; capture with `-benchmem` and `benchstat` for comparisons.

## Optimization levers (documented)
1. `--workers` (W) — raw parallelism across items/hosts.
2. Host-interleaved fan-out (already implemented) — avoids head-of-line blocking on a slow host.
3. ResponseCache single-fetch reuse — cross-checker fetch amortization (FR-02).
4. Financial: `--browser-tabs` (T) — the dominant concurrency knob for single-session scans.
5. (Backlog) retryablehttp bounded retries — converts transient failures into successes without raising per-host pressure (retries must charge the rate budget).
