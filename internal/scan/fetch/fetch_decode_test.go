package fetch

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hongsam14/chiller/internal/scan/safety"
)

// FR-T1: a br/zstd/gzip/deflate-encoded response is decoded to its real bytes before
// it reaches the HAR — the fix for the silent global false-negative (a body-based
// regex would otherwise scan compressed bytes and match nothing).
func TestFetcher_DecodesContentEncoding(t *testing.T) {
	payload := []byte("<html><!-- eyJhbGciOiJIUzI1NiJ9.eyJ1c2VyIjoiYWRtaW4ifQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJVadQssw5c --></html>")
	cases := []struct {
		name, ce string
		comp     func([]byte) []byte
	}{
		{"gzip", "gzip", gz},
		{"br", "br", brc},
		{"zstd", "zstd", zst},
		{"deflate", "deflate", zl},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Encoding", c.ce)
				_, _ = w.Write(c.comp(payload))
			}))
			defer srv.Close()
			tgt := target(t, srv.URL+"/")
			gate := safety.NewApprovalGate([]string{tgt.Host}, nil)
			f := NewFetcher(gate, safety.NewRateLimiter(0, 0), NewInMemoryCache(), srv.Client())

			e, err := f.Fetch(context.Background(), "s", tgt)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(e.Body, payload) {
				t.Errorf("%s: body not decoded — got %q", c.name, e.Body)
			}
			if e.Truncated {
				t.Errorf("%s: unexpected Truncated on a small body", c.name)
			}
		})
	}
}

// FR-T3: a body over the 512 KiB cap is truncated to the cap and flagged.
func TestFetcher_OversizedBody_Truncated(t *testing.T) {
	big := bytes.Repeat([]byte("A"), int(defaultMaxBody)+50*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(big) // identity, but larger than the cap
	}))
	defer srv.Close()
	tgt := target(t, srv.URL+"/")
	gate := safety.NewApprovalGate([]string{tgt.Host}, nil)
	f := NewFetcher(gate, safety.NewRateLimiter(0, 0), NewInMemoryCache(), srv.Client())

	e, err := f.Fetch(context.Background(), "s", tgt)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(e.Body)) != defaultMaxBody || !e.Truncated {
		t.Errorf("oversized body: len=%d truncated=%v, want %d/true", len(e.Body), e.Truncated, defaultMaxBody)
	}
}

// FR-T1/T3: an unknown Content-Encoding is surfaced as truncated (present-but-undecoded),
// never mistaken for clean text.
func TestFetcher_UnknownEncoding_Truncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "made-up-codec")
		_, _ = w.Write([]byte("cannot decode this"))
	}))
	defer srv.Close()
	tgt := target(t, srv.URL+"/")
	gate := safety.NewApprovalGate([]string{tgt.Host}, nil)
	f := NewFetcher(gate, safety.NewRateLimiter(0, 0), NewInMemoryCache(), srv.Client())

	e, err := f.Fetch(context.Background(), "s", tgt)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Truncated {
		t.Error("unknown Content-Encoding must be marked Truncated")
	}
}
