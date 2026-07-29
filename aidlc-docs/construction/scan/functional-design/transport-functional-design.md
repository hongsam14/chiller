# Functional Design — P0 Transport Correctness (r17)

Unit `scan` (fetch) + a report-surface touch. Implements `requirements-transport.md`.
NFR design (decompression-bomb bound, total decode) is folded in here.

## 1. Data model (all additive — interfaces/BSON back-compat preserved)

- **`domain.HAREntry.Truncated bool`** (`bson:"truncated,omitempty" json:"truncated,omitempty"`)
  — the body is **not fully available as decoded text**: the decompressed output hit the
  cap, a decode error left it partial, OR the `Content-Encoding` was unknown/unsupported
  (present but undecoded). Old docs decode as false.
- **`domain.WorkItemOutcome.Truncated bool`** (additive) — set by exec from the URL-scoped
  item's `Response.Truncated`, so the truncation survives the fold to finalize.
- **`report.Report.TruncatedURLs []string`** (additive) — the URLs whose body was
  truncated/partial. Empty ⇒ every scanned body was complete.

## 2. Decoder — `internal/scan/fetch/decode.go` (new)

```go
// decodeBody streams src through the codec named by contentEncoding, returning at
// most max decoded bytes. truncated is true when the output was capped, the stream
// ended in an error (partial), or the encoding is unsupported/unreadable as text.
// TOTAL: never panics, never returns an error — a body is always produced.
func decodeBody(src io.Reader, contentEncoding string, max int64) (out []byte, truncated bool)
```

- **Codec table** (case-insensitive, trims `x-` prefix): `gzip`→`compress/gzip`;
  `deflate`→`compress/zlib` then fall back to raw `compress/flate` (the encoding is
  ambiguous in the wild); `br`→`andybalholm/brotli`; `zstd`→`klauspost/compress/zstd`;
  `identity`/empty→pass-through (not truncated).
- **Bomb defense (NFR-T1):** the codec reader is wrapped `io.LimitReader(codec, max+1)`
  and `io.ReadAll`-ed. If the result is `> max`, keep `max` bytes and set `truncated`.
  Output is bounded regardless of the compressed input's expansion ratio, and the codec
  only pulls from `src` as far as producing `max+1` bytes needs (streaming — a small bomb
  never materializes in memory).
- **Total (NFR-T2 / FR-T5):** a codec-construction error (bad header) or a mid-stream
  read error returns the bytes decoded so far with `truncated=true`; never an error.
- **Unknown encoding (FR-T1):** returns the raw bytes with `truncated=true` — surfaced,
  never mistaken for clean text.

## 3. Fetch flow change — `fetch.go`

1. **Advertise** `Accept-Encoding: gzip, deflate, br, zstd` on the request, and set
   `Transport.DisableCompression = true` (clone as with the proxy path) so Go's transport
   does **not** implicitly add gzip / auto-decode — decoding is uniform and ours.
   (retryablehttp `RespReadLimit` is irrelevant here — verified against v1.3.20: it bounds
   only the retry-drain, `do.go` `io.Copy(io.Discard, io.LimitReader(resp.Body, RespReadLimit))`,
   NOT the returned body, which streams freely. `decodeBody` streams from `resp.Body` and
   enforces the sole 512 KiB *output* cap — there is no separate compressed-read cap.)
2. Replace `body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))` with
   `body, truncated := decodeBody(resp.Body, resp.Header.Get("Content-Encoding"), maxBody)`.
3. `toHAR(...)` records `Truncated: truncated`.

The safety envelope is untouched: gate/redirect-regate, per-host budget, per-request
timeout, retry token-charge all unchanged — the change is only how the body is read.

## 4. Truncation propagation (FR-T4)

```
fetch: HAREntry.Truncated ──► exec: ScanContext.Response.Truncated
   └─► WorkItemOutcome.Truncated  ──► finalize: collect item URLs where Truncated
        └─► report.Report.TruncatedURLs  (set AFTER ReportBuilder.Build)
```

- **exec** (`exec.go`, URL-scoped branch): `out.Truncated = html.Truncated || any(js[i].Truncated)`
  — a truncated discovered first-party JS asset (which PS-009 scans for secrets) counts too,
  not only the main response (review F2).
- **finalize** (`finalize.go`): after `rep := o.cfg.ReportBuilder.Build(...)`, walk the
  outcomes; for each `oc.Truncated`, resolve its item URL from the `AllItems` map (the
  same map `itemScopes` already reads) and append to a de-duplicated, sorted list; assign
  `rep.TruncatedURLs`. The `Builder` interface is **unchanged** — the field is set by the
  orchestrator, so every framework builder gets it for free.
- The report view / `/api/report` renders a "partially scanned (body truncated)" note for
  those URLs so a body-based "no finding" there is not read as authoritative.

## 5. Property-based tests (NFR-T3, FULL)

- `decode(compress(x)) == x` for random `x` within cap, for all four codecs — and for
  `deflate` in BOTH forms (zlib-wrapped and raw flate) to exercise the fallback.
- output length is always `≤ max`; a `> max` input yields exactly `max` + `truncated`.
- a highly-compressible bomb (e.g. `max*1000` zero bytes) yields a bounded, truncated
  result and does not OOM.
- random/malformed compressed bytes never panic; return `truncated` with partial output.
- identity / absent / unknown encodings behave per FR-T1.

## 6. Edge cases

- Host-scoped items have no `Response` (own protocol I/O) → never truncated.
- A `Content-Encoding` list (`br, gzip`) is decoded outermost-last per RFC; MVP handles a
  single encoding and marks a multi-encoding body `truncated` if the inner layer is still
  compressed (rare; recorded, not silently clean).
- Empty body → not truncated.

## Invariants

`Fetcher` interface unchanged (additive `HAREntry.Truncated`) · `WorkItemOutcome` /
`Report` additive fields · 26-item partition · CWE ownership · rate/gate/timeout envelope ·
product Dockerfile · platform seams · honest accounting (a truncated body is surfaced,
never hidden).
