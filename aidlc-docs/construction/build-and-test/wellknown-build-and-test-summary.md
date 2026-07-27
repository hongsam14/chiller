# Build and Test Summary — Observation well-known provider (r10)

**Increment**: Observation 고도화 — well-known-path GET provider 배선 + dictionary 심화 (6개 항목),
including the workflow code-review fix pass (10 findings). Single unit = `scan` (+ `platform/orchestrator`
fan-out/fold, `domain.WorkItem`, `ingest.ScanOptions`). No new deployable; product Dockerfile/interfaces unchanged.

## What changed (test-relevant)
- **New**: `internal/scan/checking/observation/wellknown.go` (`WellKnownPaths()`), fan-out synthesis of
  well-known URL-scoped WorkItems, 5 signature v2 anchors + review-hardened dictionaries/guards,
  `domain.WorkItem.WellKnown`, `ScanOptions.DisableWellKnown` / `MaxWellKnownHosts`.
- **Review fixes folded in**: env path guard, env FN restoration, backup soft-404 gate + broadened
  `backupName`, admin positive-content marker, `loginMarker` de-substring, robots segment anchoring,
  well-known observation-only execution + fold reconstruction alignment, sweep cap/off-switch, full
  URL-set interleave.

## Local gates — ALL GREEN (2026-07-24)
| Gate | Command | Result |
|---|---|---|
| Build | `go build ./...` | OK |
| Vet | `go vet ./...` | OK |
| Vet (integration tag) | `go vet -tags integration ./...` | OK |
| Format | `gofmt -l internal/ test/ cmd/` | clean (empty) |
| Architecture | `make arch-check` | OK (data-plane ⊥ platform, kernel ⊥ units) |
| Unit + integration (race) | `go test -race ./...` | 23 pkg **ok**, 0 FAIL |
| SAST | `gosec -quiet ./...` | 0 findings |

## Test evidence added this increment
- **FP/FN corpus** `observation/wellknown_sig_test.go` (`TestWellKnown_Corpus`) — blocking. Covers every
  widened anchor's FN **and** paired FP, plus the review regressions:
  - env: APP_KEY / short secret / weak-real-secret FN; not-.env-path, debug page, `.env.example` template FP.
  - backup: `site-backup.zip` / `db_backup.tar.gz` / `backup-2024.tar.gz` / `wwwroot.zip` FN;
    `sandbox.zip` bounded-token FP; soft-404 (HTML body **and** `text/html` Content-Type) FP.
  - admin: `Last login:` prose / `/wp-login.php` nav-link FN (genuinely open panel still fires);
    soft-404 homepage / sign-in title FP.
  - robots: `/db/…` `/api/…` segment FN; `/feedback` `/cardboard` `/rapid-delivery` substring FP.
- **Executor filter** `scan/exec/exec_test.go` (`TestRun_WellKnown_ObservationOnly`) — a well-known item
  runs the observation signature but NOT the probe adapter; a normal URL item still runs the probe.
- **Fan-out** `platform/orchestrator/orchestrator_test.go`:
  - `TestFanOut_URLAndHostCounts_Interleaved` — strengthened: max same-host run ≤ 2 across all URL items
    (detects a host-major append regression).
  - `TestFanOut_WellKnown_Disabled` — off-switch suppresses the whole sweep.
  - `TestFanOut_WellKnown_HostCap` — `MaxWellKnownHosts` bounds distinct hosts swept.
- **Honest accounting invariant** `test/platform_e2e_test.go`
  (`TestE2E_ProbesWithoutBinaries_ToolFailed_NotChecked`) — still green: 9 probe-owned unjudged
  (no binaries), 4 delegated, 13 observation judged; the well-known observation-only execution does
  **not** falsely credit skipped probes as Checked (fold reconstruction now mirrors exec).

## In-image / integration (Docker-delegated, unchanged from r10)
- `//go:build integration` `test/wellknown_integration_test.go` asserts the 6 owning CWEs surface on the
  `test/fixtures/vuln-target/` well-known surfaces (651/538/527/942/530/306). Runs in the probe-integration
  CI job / local Docker; not part of the plain `go test` gate above.

## Invariants preserved
26-item coverage partition · one-signature-one-CWE ownership · checker/platform interfaces · product
Dockerfile · probe wiring · platform in-memory seams. Carried forward (honest): PS-009/018/023 dictionary
(C1=A next todo) · P0 transport `br`/`zstd` + 512KB (C3=A) · well-known shares per-host rate budget
(+~3s/host, intended Q6=A).
