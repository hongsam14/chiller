# Code Generation Plan — P0 Transport Correctness (r17)

Branch `feat/transport-correctness`. Implements `transport-functional-design.md`
(review-corrected: F1 streaming output-bound, F2 JS propagation, F3 broadened
`Truncated`, F4 deflate both forms). Execute Part 2 in this order.

## A. Dependencies
- [x] **A1** `go get github.com/andybalholm/brotli@<latest>` + `github.com/klauspost/compress@<latest>`
      (zstd sub-package); `go mod tidy`. gzip/zlib/flate are stdlib. Pin exact versions.

## B. Decoder — `internal/scan/fetch/decode.go` (new)
- [x] **B1** `decodeBody(src io.Reader, contentEncoding string, max int64) (out []byte, truncated bool)`
      — TOTAL (no panic, no error). Codec dispatch (case-insensitive, trim `x-`):
      `gzip`→gzip.NewReader; `deflate`→zlib.NewReader, fallback raw flate.NewReader;
      `br`→brotli.NewReader; `zstd`→zstd.NewReader; ``/`identity`→pass-through(not truncated);
      unknown→raw bytes + `truncated=true`. Bomb-safe: `io.ReadAll(io.LimitReader(codec, max+1))`,
      if `len>max` keep `max` + truncated; codec-construct error or mid-stream read error →
      bytes-so-far + truncated. Streams from `src` (resp.Body streams freely — RespReadLimit
      is retry-drain only, verified).

## C. Data model (additive — BSON/interface back-compat)
- [x] **C1** `domain.HAREntry.Truncated bool` (`bson:"truncated,omitempty" json:"truncated,omitempty"`),
      doc-comment = "body not fully available as decoded text" (F3 broadened meaning).
- [x] **C2** `domain.WorkItemOutcome.Truncated bool` (additive).
- [x] **C3** `report.Report.TruncatedURLs []string` (additive, omitempty).

## D. Fetch wiring — `internal/scan/fetch/fetch.go`
- [x] **D1** Set `Accept-Encoding: gzip, deflate, br, zstd` on the request; set
      `Transport.DisableCompression = true` (clone/coexist with the proxy-transport path).
      Replace `io.ReadAll(io.LimitReader(resp.Body, f.maxBody))` with
      `decodeBody(resp.Body, resp.Header.Get("Content-Encoding"), f.maxBody)`; `toHAR` sets
      `Truncated`. Safety envelope (gate/redirect-regate/rate/timeout/retry-charge) untouched.

## E. Truncation propagation (FR-T4)
- [x] **E1** `exec.go` (URL-scoped branch): `out.Truncated = html.Truncated || any discovered
      JS truncated` (F2 — a truncated first-party JS asset counts, PS-009 source).
- [x] **E2** `finalize.go`: after `ReportBuilder.Build(...)`, collect item URLs for outcomes
      where `Truncated` (via the `AllItems` map `itemScopes` already reads), dedup + sort,
      assign `rep.TruncatedURLs`. `Builder` interface UNCHANGED (orchestrator sets the field).
- [x] **E3** Report view (`internal/platform/api/introspect.go` report UI + `/report` render):
      show a "partially scanned — body truncated" note listing `TruncatedURLs`, so a
      body-based "no finding" on those URLs is not read as authoritative.

## F. Verification
- [x] **F1** Decoder PBT (`decode_test.go`): `decode(compress(x))==x` for gzip/deflate(zlib
      AND raw)/br/zstd within cap; output always ≤ max; a high-ratio bomb (`max*1000` zeros)
      → bounded + truncated, no OOM; random/malformed bytes never panic → truncated+partial;
      identity/absent/unknown per FR-T1/T3.
- [x] **F2** Fetch tests (`fetch_test.go`): a `br`/`zstd`/`gzip`-encoded response body decodes
      to the expected text (proves the global-FN fix); an oversized decoded body → `Truncated`;
      an unknown `Content-Encoding` → `Truncated` + raw bytes; identity unchanged.
- [x] **F3** Propagation tests: exec sets `out.Truncated` from a truncated main body AND from a
      truncated discovered JS (F2); finalize maps truncated outcomes → `rep.TruncatedURLs`
      (dedup/sort); report view renders the note.
- [x] **F4** Gates: build · vet(+integration) · gofmt · `make arch-check` · `go test -race` ·
      `make gosec` · **`make vuln`** (new deps) · `make secrets` · `make signatures-check`.

## Risk / invariants
Medium. Hot fetch path + 2 new decompression deps + bomb surface (defended by output bound)
+ report field. `Fetcher` interface unchanged (additive `HAREntry.Truncated`); `WorkItemOutcome`/
`Report` additive; 26-partition · CWE ownership · rate/gate/timeout envelope · Dockerfile · seams
unchanged. Widest change = fetch body-read; honesty preserved (truncation surfaced, never hidden).

## 완료 노트 (Part 2 실행 결과)
- A~F 전 단계 구현·검증 완료. 전체 스위트 green.
- 게이트: build·vet(+integration)·gofmt·arch-check·`go test -race`·**gosec 0**(G110 decompression-DoS 미발생 — 출력 bound)·`make secrets`(no leaks)·`make signatures-check`(drift 0).
- **`make vuln`(reported-not-blocking)**: 신규 deps(brotli·klauspost/compress) **0 취약점**. 기존 `golang.org/x/text@v0.37.0`의 GO-2026-5970은 **r17 무관(main에도 동일, 미변경)** — 후속으로 x/text→v0.40.0 범프 권장(별도, 이 증분 범위 밖).
- 검증: 디코더 PBT(round-trip 5코덱+deflate 2형태·cap·bomb 64MiB→bounded·malformed total·identity/unknown) · fetch br/zstd/gzip/deflate 디코드 + oversized/unknown truncation · 전파(exec main‖JS·finalize truncatedURLs·report DTO) · report view 노트.
- **F2(리뷰 지적) 회귀 방어**: `TestRun_Truncated_MainOrJS`가 잘린 JS 에셋도 outcome.Truncated 세팅함을 고정.
