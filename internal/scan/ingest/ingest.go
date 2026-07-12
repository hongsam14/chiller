// Package ingest validates a scan request into an in-scope URL set (US-003,
// SR-1). No crawling/discovery here (FR-01); approval is enforced by the Safety
// ApprovalGate before any outbound request (SR-3), not duplicated here.
package ingest

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/enki-offen/chiller/internal/domain"
)

// ScanRequest is the raw input.
type ScanRequest struct {
	URLs     []string    `bson:"urls"`
	Approved []string    `bson:"approved"`
	Options  ScanOptions `bson:"options"`
}

// ScanOptions parameterize a scan (SR-4 rate, SR-1 bound).
type ScanOptions struct {
	RatePerHost int      `bson:"ratePerHost"`
	Burst       int      `bson:"burst"`
	MaxURLs     int      `bson:"maxURLs"`
	Tools       []string `bson:"tools"`
}

// Safe defaults applied to unset (zero) options so an unconfigured scan is never
// unbounded — a zero RatePerHost would otherwise disable per-host throttling
// entirely (SR-4).
const (
	defaultRatePerHost = 5   // requests/sec/host
	defaultMaxURLs     = 500 // per-scan URL ceiling (SR-1)
)

// withDefaults fills unset options with safe values. Burst defaults to the rate.
func (o ScanOptions) withDefaults() ScanOptions {
	if o.RatePerHost == 0 {
		o.RatePerHost = defaultRatePerHost
	}
	if o.Burst == 0 {
		o.Burst = o.RatePerHost
	}
	if o.MaxURLs == 0 {
		o.MaxURLs = defaultMaxURLs
	}
	return o
}

// URLSet is the validated, in-scope target set with the effective (defaulted)
// options the caller must use to build the rate limiter and bounds.
type URLSet struct {
	URLs    []domain.Target
	Options ScanOptions
}

// Ingestor is the seam (NFR-01).
type Ingestor interface {
	Ingest(ScanRequest) (URLSet, error)
}

type ingestor struct{}

// New builds an Ingestor.
func New() Ingestor { return &ingestor{} }

func (i *ingestor) Ingest(req ScanRequest) (URLSet, error) {
	if req.Options.RatePerHost < 0 || req.Options.Burst < 0 || req.Options.MaxURLs < 0 {
		return URLSet{}, errors.New("ingest: option out of range (negative)")
	}
	// Apply defaults BEFORE enforcing the ceiling, so a request that omits maxURLs
	// (zero) is bounded by the default 500 (SR-1) — not left uncapped. The
	// effective options are also what URLSet advertises, so check and advertise
	// must use the same defaulted value.
	opts := req.Options.withDefaults()
	if len(req.URLs) > opts.MaxURLs {
		return URLSet{}, fmt.Errorf("ingest: %d URLs exceeds MaxURLs %d", len(req.URLs), opts.MaxURLs)
	}
	out := make([]domain.Target, 0, len(req.URLs))
	for _, raw := range req.URLs {
		t, err := parseTarget(raw)
		if err != nil {
			return URLSet{}, fmt.Errorf("ingest: %w", err) // fail-closed on any bad URL (SR-1)
		}
		out = append(out, t)
	}
	return URLSet{URLs: out, Options: opts}, nil
}

func parseTarget(raw string) (domain.Target, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return domain.Target{}, fmt.Errorf("malformed URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return domain.Target{}, fmt.Errorf("URL %q: scheme must be http/https", raw)
	}
	if u.Host == "" {
		return domain.Target{}, fmt.Errorf("URL %q: missing host", raw)
	}
	return domain.Target{URL: u.String(), Host: u.Hostname(), Scheme: u.Scheme}, nil
}
