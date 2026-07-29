package fetch

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"io"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// decodeBody streams src through the codec named by contentEncoding and returns at
// most max decoded bytes plus whether the body is NOT fully available as decoded
// text (r17 FR-T3): the decoded output hit the cap, a decode error left it partial,
// or the encoding is unknown/unsupported (present but undecoded).
//
// It is TOTAL: it never panics and never returns an error — a []byte is always
// produced. Go's default transport only auto-decompresses gzip (and only when it
// implicitly asked for it); a br/zstd/deflate response therefore reaches us
// compressed, and scanning those bytes as text matches nothing (a silent global
// false-negative). Decoding here closes that gap.
//
// Bomb defense (FR-T2/NFR-T1): the codec reader is bounded to max+1 output bytes and
// only pulls from src as far as producing that needs, so a small "zip bomb" never
// materializes in memory regardless of its expansion ratio.
func decodeBody(src io.Reader, contentEncoding string, max int64) (out []byte, truncated bool, err error) {
	switch normalizeEncoding(contentEncoding) {
	case "", "identity":
		// Uncompressed: read bounded. A genuine read error (network reset, TLS error,
		// ctx deadline) is PROPAGATED so Fetch fails the item (tool-failed), preserving
		// the pre-r17 honesty contract — this is not a decode/format problem.
		b, overflow, rerr := readBounded(src, max)
		return b, overflow, rerr

	case "gzip":
		b, t := decodeStream(src, max, func(r io.Reader) (io.Reader, func(), error) {
			gr, gerr := gzip.NewReader(r)
			if gr == nil {
				return nil, nil, gerr
			}
			return gr, func() { _ = gr.Close() }, gerr
		})
		return b, t, nil

	case "br":
		b, t := decodeStream(src, max, func(r io.Reader) (io.Reader, func(), error) {
			return brotli.NewReader(r), nil, nil
		})
		return b, t, nil

	case "zstd":
		b, t := decodeStream(src, max, func(r io.Reader) (io.Reader, func(), error) {
			zr, zerr := zstd.NewReader(r)
			if zr == nil {
				return nil, nil, zerr
			}
			return zr.IOReadCloser(), func() { zr.Close() }, zerr
		})
		return b, t, nil

	case "deflate":
		b, t := decodeDeflate(src, max)
		return b, t, nil

	default:
		// Unknown/unsupported encoding: present but undecoded. Return the raw bytes
		// (bounded) and mark truncated so it is never mistaken for clean text (FR-T3).
		b, _, _ := readBounded(src, max)
		return b, true, nil
	}
}

// normalizeEncoding lowercases, trims, and strips an `x-` vendor prefix. A
// multi-value Content-Encoding (e.g. "br, gzip") does not match a single codec and
// falls through to the unknown branch (recorded as truncated, not silently clean).
func normalizeEncoding(ce string) string {
	e := strings.ToLower(strings.TrimSpace(ce))
	return strings.TrimPrefix(e, "x-")
}

// decodeStream reads from a codec reader bounded to max, treating overflow or any
// read/construction error as truncated (FR-T5: partial + continue, never an error).
func decodeStream(src io.Reader, max int64, mk func(io.Reader) (io.Reader, func(), error)) ([]byte, bool) {
	r, cleanup, err := mk(src)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil || r == nil {
		return nil, true // bad header → nothing decoded, surfaced as truncated
	}
	b, overflow, rerr := readBounded(r, max)
	return b, overflow || rerr != nil
}

// decodeDeflate handles the ambiguous `deflate` encoding (zlib-wrapped, RFC-correct,
// vs raw flate) by PEEKING the first two bytes to pick the codec, then STREAMING the
// chosen decoder output-bounded (like gzip/br/zstd) — so it never bounds the compressed
// input to the output cap (a large-but-decodable body is not falsely truncated), and it
// never re-reads a valid zlib stream as raw flate (which would decode its header to
// garbage). A truncated-but-valid zlib stream keeps its recoverable prefix.
func decodeDeflate(src io.Reader, max int64) ([]byte, bool) {
	var hdr [2]byte
	n, _ := io.ReadFull(src, hdr[:])
	r := io.MultiReader(bytes.NewReader(hdr[:n]), src) // put the peeked bytes back
	if n == 2 && isZlibHeader(hdr[0], hdr[1]) {
		return decodeStream(r, max, func(rr io.Reader) (io.Reader, func(), error) {
			zr, err := zlib.NewReader(rr)
			if zr == nil {
				return nil, nil, err
			}
			return zr, func() { _ = zr.Close() }, err
		})
	}
	return decodeStream(r, max, func(rr io.Reader) (io.Reader, func(), error) {
		fr := flate.NewReader(rr)
		return fr, func() { _ = fr.Close() }, nil
	})
}

// isZlibHeader reports whether (cmf, flg) is a valid zlib (RFC 1950) header: deflate
// compression method (CM=8), window size CINFO≤7, and the 16-bit check FCHECK so that
// (CMF*256+FLG) is a multiple of 31. Raw flate blocks almost never satisfy this.
func isZlibHeader(cmf, flg byte) bool {
	return cmf&0x0f == 8 && cmf>>4 <= 7 && (uint16(cmf)<<8|uint16(flg))%31 == 0
}

// readBounded reads up to max bytes from r. overflow is true when r had more than
// max bytes available (the extra is dropped). It reads max+1 so a body exactly at
// the cap is not misreported as overflowing.
func readBounded(r io.Reader, max int64) (out []byte, overflow bool, err error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if int64(len(b)) > max {
		return b[:max], true, err
	}
	return b, false, err
}
