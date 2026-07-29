package fetch

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"io"
	"testing"
	"testing/quick"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// --- codec compressors (test-side) ---

func gz(b []byte) []byte {
	var o bytes.Buffer
	w := gzip.NewWriter(&o)
	_, _ = w.Write(b)
	_ = w.Close()
	return o.Bytes()
}
func zl(b []byte) []byte {
	var o bytes.Buffer
	w := zlib.NewWriter(&o)
	_, _ = w.Write(b)
	_ = w.Close()
	return o.Bytes()
}
func fla(b []byte) []byte {
	var o bytes.Buffer
	w, _ := flate.NewWriter(&o, flate.DefaultCompression)
	_, _ = w.Write(b)
	_ = w.Close()
	return o.Bytes()
}
func brc(b []byte) []byte {
	var o bytes.Buffer
	w := brotli.NewWriter(&o)
	_, _ = w.Write(b)
	_ = w.Close()
	return o.Bytes()
}
func zst(b []byte) []byte {
	var o bytes.Buffer
	w, _ := zstd.NewWriter(&o)
	_, _ = w.Write(b)
	_ = w.Close()
	return o.Bytes()
}

const cap1MB = 1 << 20

// FR-T1 round-trip: decode(compress(x)) == x for every codec, incl. deflate in BOTH
// the zlib-wrapped and raw-flate forms (exercises decodeDeflate's fallback). Property.
func TestDecode_RoundTrip(t *testing.T) {
	codecs := []struct {
		name, enc string
		comp      func([]byte) []byte
	}{
		{"gzip", "gzip", gz},
		{"deflate-zlib", "deflate", zl},
		{"deflate-raw", "deflate", fla},
		{"br", "br", brc},
		{"zstd", "zstd", zst},
	}
	for _, c := range codecs {
		c := c
		t.Run(c.name, func(t *testing.T) {
			prop := func(x []byte) bool {
				out, trunc, _ := decodeBody(bytes.NewReader(c.comp(x)), c.enc, cap1MB)
				return !trunc && bytes.Equal(out, x)
			}
			if err := quick.Check(prop, &quick.Config{MaxCount: 300}); err != nil {
				t.Errorf("%s round-trip failed: %v", c.name, err)
			}
		})
	}
}

// FR-T2: output is always capped, and a body over the cap reports truncated.
func TestDecode_OutputCapped(t *testing.T) {
	big := bytes.Repeat([]byte("A"), 1000)
	out, trunc, _ := decodeBody(bytes.NewReader(gz(big)), "gzip", 100)
	if int64(len(out)) != 100 || !trunc {
		t.Errorf("len=%d trunc=%v, want 100/true", len(out), trunc)
	}
	// exactly-at-cap must NOT be reported truncated (the +1 read guards this).
	exact := bytes.Repeat([]byte("B"), 100)
	out2, trunc2, _ := decodeBody(bytes.NewReader(gz(exact)), "gzip", 100)
	if len(out2) != 100 || trunc2 {
		t.Errorf("exact-at-cap: len=%d trunc=%v, want 100/false", len(out2), trunc2)
	}
}

// NFR-T1: a decompression bomb (tiny compressed, huge decompressed) yields a bounded,
// truncated result and does not exhaust memory.
func TestDecode_BombBounded(t *testing.T) {
	bomb := gz(bytes.Repeat([]byte{0}, 64*1024*1024)) // 64 MiB of zeros → ~64 KiB gzip
	out, trunc, _ := decodeBody(bytes.NewReader(bomb), "gzip", 512*1024)
	if int64(len(out)) > 512*1024 || !trunc {
		t.Errorf("bomb not bounded: len=%d trunc=%v", len(out), trunc)
	}
}

// FR-T5 / NFR-T2: malformed compressed input never panics; the result is bounded, and
// the codecs with a validating header report truncated.
func TestDecode_MalformedIsTotal(t *testing.T) {
	junk := []byte("this is not a valid compressed stream, at all, 0123456789")
	for _, enc := range []string{"gzip", "br", "zstd", "deflate"} {
		out, _, _ := decodeBody(bytes.NewReader(junk), enc, cap1MB) // must not panic
		if int64(len(out)) > cap1MB {
			t.Errorf("%s: output exceeded cap", enc)
		}
	}
	// header-validating codecs must surface malformed input as truncated (undecodable).
	for _, enc := range []string{"gzip", "zstd"} {
		if _, trunc, _ := decodeBody(bytes.NewReader(junk), enc, cap1MB); !trunc {
			t.Errorf("%s: malformed input should be truncated", enc)
		}
	}
}

// errReader yields data then a non-EOF read error (a mid-stream network failure).
type errReader struct{ data []byte }

func (r *errReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	return 0, io.ErrUnexpectedEOF
}

// F2 (review): a genuine read error on the identity path is PROPAGATED (so Fetch fails
// the item = tool-failed), not swallowed into truncated — preserving the honesty contract.
func TestDecode_IdentityReadError_Propagates(t *testing.T) {
	_, _, err := decodeBody(&errReader{data: []byte("partial body")}, "", cap1MB)
	if err == nil {
		t.Error("a mid-stream read error on an uncompressed body must be returned, not swallowed")
	}
}

// F3 (review): a valid-but-truncated zlib deflate stream keeps its recoverable PREFIX
// (via the zlib decoder), instead of being re-read as raw flate and decoded to garbage.
func TestDecode_DeflateTruncatedZlib_KeepsPrefix(t *testing.T) {
	full := bytes.Repeat([]byte("SECRETtoken-eyJ-"), 64) // 1024 bytes
	comp := zl(full)                                     // zlib-wrapped
	truncated := comp[:len(comp)-4]                      // drop the adler32 trailer
	out, trunc, _ := decodeBody(bytes.NewReader(truncated), "deflate", cap1MB)
	if !trunc {
		t.Error("truncated zlib stream should be marked truncated")
	}
	if len(out) == 0 || !bytes.HasPrefix(full, out) {
		t.Errorf("truncated zlib must decode a valid prefix (not flate-misread garbage); got %d bytes", len(out))
	}
}

// FR-T1/T3: identity/absent pass through un-truncated; an unknown encoding returns the
// raw bytes but is marked truncated (present-but-undecoded, never mistaken for clean).
func TestDecode_IdentityAndUnknown(t *testing.T) {
	raw := []byte("<html><!-- secret: eyJ... --></html>")
	for _, enc := range []string{"", "identity"} {
		out, trunc, _ := decodeBody(bytes.NewReader(raw), enc, cap1MB)
		if trunc || !bytes.Equal(out, raw) {
			t.Errorf("enc=%q: want raw/untruncated, got trunc=%v", enc, trunc)
		}
	}
	out, trunc, _ := decodeBody(bytes.NewReader(raw), "weird-codec", cap1MB)
	if !trunc || !bytes.Equal(out, raw) {
		t.Errorf("unknown encoding: want raw bytes + truncated, got trunc=%v", trunc)
	}
}
