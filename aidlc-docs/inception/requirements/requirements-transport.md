# Requirements — P0 Transport Correctness (r17)

**Increment**: fix the two transport blind spots carried forward since r10/r16 —
compressed response bodies are never decoded, and the 512 KiB body cap truncates
silently. Together they are the single largest correctness gap in the detection
engine (a `br`-emitting path nullifies every body-based signature).

**Unit**: `scan` (single) — primarily `internal/scan/fetch`, with a report-surface
touch in `findings`/`platform` for truncation exposure. No new deployable. The
`fetch.Fetcher` interface is unchanged (PR: additive `HAREntry` field only).

## Problem (grounded in code)

- `fetch.go:198` reads the body with `io.ReadAll(io.LimitReader(resp.Body, 512KB))`.
  Go's default transport auto-decompresses **only gzip, and only when it implicitly
  added `Accept-Encoding: gzip` itself**. A server or proxy that replies with
  `br` (Cloudflare's default), `zstd`, or `deflate` hands us **compressed bytes**,
  which every body regex (PS-003/009/018, robots/env/crossdomain markers, …) scans
  and matches nothing — a silent, global false-negative.
- The 512 KiB `LimitReader` truncates large bundles with **no record**, so an anchor
  past the cut is missed and the item is still reported "clean".

## Decisions (Requirements Analysis, 2026-07-29)

- **Q1 = A — explicit decoding.** Advertise `Accept-Encoding: gzip, deflate, br, zstd`,
  disable the transport's implicit decompression, and decode the body ourselves by
  `Content-Encoding`. Robust regardless of what a CDN/proxy returns. New deps:
  brotli (`andybalholm/brotli`), zstd (`klauspost/compress/zstd`); gzip/flate are stdlib.
- **Q2 = A — decompressed output capped at 512 KiB.** Bound the decompressed OUTPUT at
  the existing scan budget by streaming the body through the codec into an output limit
  (decompression-bomb defense). The compressed input is naturally bounded by the output
  cap — no separate compressed-size limit (retryablehttp's `RespReadLimit` bounds only
  retry-drain, not the returned body; verified against v1.3.20 `do.go`).
- **Q3 = B — record AND surface in the report.** A truncated body must never read as a
  complete "clean" scan: record it on the fetched artifact and surface the affected
  URLs in the report.
- **Q4 = A — partial data on decode failure.** A broken/malformed compressed stream
  keeps the bytes decoded so far and continues scanning (marked truncated); it does not
  fail the fetch (a partial body still yields real findings).

## Functional Requirements

- **FR-T1** — Decode the response body by `Content-Encoding` (`gzip`, `deflate`, `br`,
  `zstd`, and the identity/absent case) before it reaches any checker. Set our own
  `Accept-Encoding` and disable transport auto-decompression so decoding is uniform and
  under our control. Unknown/unsupported encodings pass through as-is and are marked
  `Truncated` (per FR-T3's meaning) so an undecoded body is never mistaken for clean text.
- **FR-T2** — Bound the decompressed OUTPUT at 512 KiB (`defaultMaxBody`) by streaming the
  body reader through the codec into an output limit, so a small bomb cannot exhaust
  memory regardless of the compressed input's expansion ratio. There is NO separate
  compressed-read cap — the streaming output bound naturally limits how much compressed
  input is consumed, and `RespReadLimit` bounds only retry-drain (not the returned body).
- **FR-T3** — `domain.HAREntry.Truncated` means **the body is not fully available as
  decoded text**. It is set whenever the decompressed output hit its cap, a decode error
  left the body partial, OR the `Content-Encoding` is unknown/unsupported (present but
  undecoded). A truncated artifact is never read as a complete, authoritative body.
- **FR-T4** — Surface truncation in the report: the report identifies URLs whose body
  was truncated/partial, so a reader can see that those pages were only partially
  scanned and must not treat a body-based "no finding" on them as authoritative. This
  includes a URL whose **main response OR any discovered first-party JS asset** was
  truncated (PS-009 scans that JS source, so a truncated JS body is also incomplete).
- **FR-T5** — A decode failure (malformed `br`/`zstd`/`gzip`/`deflate` stream) keeps the
  bytes decoded so far, marks the artifact truncated, and continues — never a fetch error
  and never a panic (total function).

## Non-Functional

- **NFR-T1 (security-baseline)** — decompression-bomb defense: bounded decompressed
  output (FR-T2); no new outbound scope, no new host contacted. Deps pinned by version +
  drift-checked by the existing `make vuln`/SBOM.
- **NFR-T2 (resiliency-baseline)** — decode is a total function: malformed input →
  partial + continue (FR-T5), never a crash or hang; the per-request timeout (SR-8) and
  per-host budget (SR-4) are unchanged.
- **NFR-T3 (PBT, FULL)** — property tests for the decoder: `decode(compress(x)) == x`
  for x within cap (all four codecs); output is always ≤ cap; a bomb input yields a
  bounded, truncated result; malformed input never panics.
- **Invariants preserved** — `Fetcher` interface (additive `HAREntry.Truncated` only) ·
  26-item coverage partition · CWE ownership · rate/gate/timeout safety envelope ·
  product Dockerfile · platform seams.

## Out of scope (carried)

- Per-finding truncation tainting beyond the report-level URL list (FR-T4 surfaces at
  the report; per-finding caveats are a later refinement if wanted).
- HTTP/2-specific or chunked-transfer concerns (orthogonal; the fetcher already handles
  them via the stdlib transport).
- Changing the 512 KiB budget itself (kept; a larger budget is a separate tuning call).

## Extension compliance (applicable rules)

| Extension | Applicable | How |
|---|---|---|
| security-baseline | Yes | NFR-T1 decompression-bomb bound; no new outbound scope |
| resiliency-baseline | Yes | NFR-T2 total decode function, partial-on-failure |
| property-based-testing | Yes | NFR-T3 decoder round-trip / cap / bomb / malformed PBTs |
