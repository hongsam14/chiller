# Unit Test Execution — chiller

Unit tests were generated alongside each unit during Code Generation. Property-based tests (the `property-based-testing` extension, FULL) back the findings invariants (P1–P7) and the observation/coverage anchors.

## Run Unit Tests

### 1. Execute All Unit Tests
```bash
go test ./...                    # all packages
go test -race ./...              # with the race detector (concurrency-heavy: workqueue, taskstore, orchestrator, proxy)
```

### 2. Review Test Results
- **Expected**: **103 test functions across 20 test packages, 0 failures.**
- **Coverage** (statements): module total **66.0%**. Notable per-package:
  | package | coverage |
  |---|---|
  | findings/normalize · report · store | 100% |
  | scan/checking/registry · platform/workqueue | 100% |
  | platform/progress | 97.9% |
  | findings/coverage · dedup | 96.2% / 96.8% |
  | scan/ingest · safety · exec | 96.3% / 95.5% / 94.6% |
  | domain | 88.5% |
  | scan/probe · observation | 86.5% / 77.4% |
  | platform/api · taskstore · fetch | 78.8% / 74.0% / 78.2% |
  | platform/orchestrator · proxy | 38.0% / 38.1% (see note) |

  **Note on orchestrator/proxy (38%)**: their end-to-end paths (per-scan executor build, worker loop, finalize/fold, financial egress) are exercised by the `test/` **integration** package, whose coverage Go attributes to the test package, not back to the unit under test. To attribute it, run:
  ```bash
  go test -coverpkg=./internal/platform/... ./internal/platform/... ./test/
  ```
  The Playwright real `BrowserSession` is deliberately CI-uncovered (needs Chromium; the `FakeSession` covers the contract).
- **Test Report Location**: stdout; add `-json` for machine-readable, `-coverprofile=cov.out` + `go tool cover -html=cov.out` for an HTML report.

### 3. Fix Failing Tests
If tests fail:
1. Re-run the single package verbosely: `go test -v -run TestName ./internal/<pkg>/`.
2. For flakiness in timing tests (`TestE2E_WorkerPoolConcurrency`, proxy cap), re-run with `-count=1` to bypass the cache; the concurrency assertions carry generous margins.
3. Fix code, re-run `go test -race ./...` until green.

## Key unit-test coverage by unit
- **domain (U0)**: Finding/Coverage validation, CWE normalization, severity ordinality, `TransportOf` default.
- **findings (U3)**: P1–P7 property tests (normalize totality, dedup idempotence, severity totality, coverage partition, projection totality, P6 coverage-invariance).
- **scan (U2)**: ingest defaults/fail-closed, safety gate + rate limiter (burst clamp, redirect re-gate), fetch timeout/redirect/proxy egress, observation signature anchors, probe transport declarations, exec fault-isolation + host-gate fail-closed.
- **platform (U1)**: taskstore CAS single-winner, workqueue ctx-block/drain, progress partition, api authz fail-closed + status codes, orchestrator fan-out + PR-5 fold + boot gate, proxy work-cap==T (keep-alive excluded) + session injection.
