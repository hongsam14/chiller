package fetch

import (
	"net/url"
	"regexp"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/scan/safety"
)

// Discoverer finds a page's JS asset URLs (SR-5). It runs as step ② of the
// Fetch+Cache phase — after the main HTML is fetched, before the discovered JS
// is fetched. Swappable seam (NFR-02):
//
//	StaticDiscoverer   (MVP)      — parses the fetched html's <script src>, no execution.
//	PlaywrightDiscoverer (Post-MVP) — loads target in Chromium, captures requested JS URLs (r5 A-6).
type Discoverer interface {
	Discover(target domain.Target, html *domain.HAREntry) []domain.Target
}

// scriptSrc matches <script ... src="..."> (single or double quotes). Conservative;
// x/net/html is a documented future improvement.
var scriptSrc = regexp.MustCompile(`(?i)<script\b[^>]*\bsrc\s*=\s*["']([^"']+)["']`)

// maxDiscoveredJS bounds per-page asset fan-out so one page cannot amplify into
// an unbounded burst of outbound fetches (bypassing the ingest MaxURLs cap).
const maxDiscoveredJS = 100

// StaticDiscoverer implements the MVP path: parse <script src>, resolve, and
// keep only approved-host JS (FR-10/GC2). No JS execution.
type StaticDiscoverer struct{ gate safety.ApprovalGate }

// NewStaticDiscoverer builds the MVP discoverer.
func NewStaticDiscoverer(gate safety.ApprovalGate) *StaticDiscoverer {
	return &StaticDiscoverer{gate: gate}
}

func (d *StaticDiscoverer) Discover(target domain.Target, html *domain.HAREntry) []domain.Target {
	if html == nil {
		return nil
	}
	base, err := url.Parse(target.URL)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []domain.Target
	for _, m := range scriptSrc.FindAllSubmatch(html.Body, -1) {
		ref, err := url.Parse(string(m[1]))
		if err != nil {
			continue
		}
		abs := base.ResolveReference(ref)
		if abs.Scheme != "http" && abs.Scheme != "https" {
			continue
		}
		if seen[abs.String()] {
			continue
		}
		seen[abs.String()] = true
		t := domain.Target{URL: abs.String(), Host: abs.Hostname(), Scheme: abs.Scheme}
		if d.gate.Authorize(t) != nil { // approved hosts only; third-party skipped (GC2)
			continue
		}
		out = append(out, t)
		if len(out) >= maxDiscoveredJS { // bound per-page fan-out (amplification guard)
			break
		}
	}
	return out
}
