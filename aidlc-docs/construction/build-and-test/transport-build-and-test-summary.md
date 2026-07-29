# Build and Test Summary — P0 Transport Correctness (r17)

**Increment**: decode `br`/`zstd`/`deflate` response bodies (the silent global false-negative)
and surface truncated/partial bodies in the report. Single unit = `scan` (`fetch`) + a
report-surface touch. Includes the workflow code-review fix pass (10 findings, 8 fixed).
`Fetcher` interface unchanged (additive `HAREntry.Truncated`).

## What changed (test-relevant)
- **New** `internal/scan/fetch/decode.go` — `decodeBody(src, contentEncoding, max)`: total codec
  dispatch (gzip/deflate=zlib-or-raw-flate via 2-byte header peek/br/zstd/identity/unknown),
  streaming output-bounded (decompression-bomb safe), returns a read error on the identity path.
- **`fetch.go`** — advertise `Accept-Encoding: gzip,deflate,br,zstd` (which alone stops Go's
  auto-gzip, so the shared transport is left untouched — no per-scan clone); decode via `decodeBody`;
  a genuine transport read error still fails the fetch (tool-failed), only decode/format → truncated.
- **Data model (additive)** — `HAREntry.Truncated`; `WorkItemOutcome.TruncatedURLs []string` (the
  exact URLs, incl. a discovered JS asset's own URL); `report.Report.TruncatedURLs`. Sealed through
  the Mongo ledger; the Valkey cache JSON-marshals `HAREntry` wholesale so `Truncated` round-trips.
- **Propagation** — exec collects truncated URLs; finalize unions them (no item-ledger re-read);
  the `/report` view renders a "partially scanned — body truncated" warning.

## Local gates — ALL GREEN (2026-07-29)
| Gate | Command | Result |
|---|---|---|
| Build | `go build ./...` | OK |
| Vet | `go vet ./...` / `-tags integration` | OK |
| Format | `gofmt -l internal/ cmd/ tools/` | clean |
| Architecture | `make arch-check` | OK (data-plane ⊥ platform, kernel ⊥ units) |
| Unit + integration (race) | `go test -race ./...` | 23 pkg **ok**, 0 FAIL |
| SAST | `make gosec` | 0 findings (no G110 decompression-DoS — output bounded) |
| Secret scan | `make secrets` (gitleaks, full history) | no leaks found |
| Signature drift | `make signatures-check` | drift 0 |
| Dependency vuln | `make vuln` (reported-not-blocking) | **new deps (brotli, klauspost) 0 vulns**; pre-existing `x/text@v0.37.0` GO-2026-5970 (fixed v0.39.0) — unchanged by r17 |

## Test evidence added
- **Decoder PBT** `decode_test.go` — `decode(compress(x))==x` for gzip/deflate(zlib AND raw)/br/zstd;
  output always ≤ cap; a 64 MiB bomb → bounded+truncated (no OOM); malformed → total (no panic);
  identity/unknown per FR-T1/T3; **read-error propagation** (F2); **truncated-zlib keeps prefix** (F3).
- **Fetch end-to-end** `fetch_decode_test.go` — a br/zstd/gzip/deflate response decodes to its real
  bytes (proves the global-FN fix); oversized → Truncated; unknown encoding → Truncated.
- **Propagation** — `exec_test.go` (main and JS truncation set the right URLs; JS reports the JS URL,
  F5; nil-fetch no panic, F7); `orchestrator_test.go` (finalize unions/dedup/sorts, no ledger re-read);
  `report_view_test.go` (DTO surfaces TruncatedURLs).

## Code-review pass (high, 10 findings)
Verified 10, fixed 8, 1 no-change-needed, 1 skipped. The two most severe were **F1** (Mongo
seal/open dropped the truncation field → FR-T4 silently no-op'd in production) and **F6** (the
transport was cloned/replaced on every scan → lost connection-pool reuse + shared-client race). See
`construction/plans/transport-code-generation-plan.md` completion note + audit.md. golden 철칙: no
violation (arch-check, RE2, fail-closed boot, honest accounting, safety envelope all intact).

## Invariants preserved
`Fetcher` interface (additive `HAREntry.Truncated`) · `WorkItemOutcome`/`Report` additive · 26-item
partition · CWE ownership · rate/gate/timeout envelope · product Dockerfile · platform seams · honest
accounting (a genuine transport read failure still fails → tool-failed; truncation is surfaced).

## Carried forward (honest)
- `make vuln` `x/text@v0.37.0` GO-2026-5970 — a separate indirect-dep bump to v0.39.0+ (not r17 scope).
- Review F9 (skipped): the decoded HAR retains the compressed `Content-Encoding`/`Content-Length`
  headers (latent — no current consumer); a small honesty follow-up to strip/fix them when decoding.
