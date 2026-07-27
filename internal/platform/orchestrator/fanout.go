package orchestrator

import (
	"fmt"
	"log"
	"net/url"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/scan/checking/observation"
	"github.com/hongsam14/chiller/internal/scan/ingest"
)

// fanOut turns a validated URLSet into WorkItems (PR-3). Two scopes:
//   - one URL-scoped item per URL (runs every URL-scoped checker over one shared
//     fetch, FR-02),
//   - one host-scoped item per distinct host (runs every host-scoped checker,
//     which do their own protocol I/O).
//
// URL items are host-interleaved (PR-3a): consecutive items target different
// hosts so one slow or rate-limited host does not head-of-line-block the others
// as workers pull from the queue.
func fanOut(scanID domain.ScanID, set ingest.URLSet, haveURL, haveHost bool) []domain.WorkItem {
	var items []domain.WorkItem
	n := 0
	nextID := func() domain.WorkItemID {
		id := domain.WorkItemID(fmt.Sprintf("%s#%d", scanID, n))
		n++
		return id
	}

	if haveURL {
		// Build every URL-scoped item first (submitted + synthesized well-known),
		// THEN interleave the whole set by host. The well-known sweep must not be
		// appended host-major after the interleaved prefix: Enqueue preserves slice
		// order, so a trailing block of N consecutive same-host items would let one
		// rate-limited host head-of-line-block every other host's work (defeating
		// the PR-3a interleave invariant this file guarantees).
		urlItems := make([]domain.WorkItem, 0, len(set.URLs))
		for _, t := range set.URLs {
			urlItems = append(urlItems, domain.WorkItem{ScanID: scanID, Scope: domain.URLScoped, Target: t})
		}
		urlItems = append(urlItems, wellKnownItems(scanID, set)...)
		for _, it := range interleaveItemsByHost(urlItems) {
			it.ID = nextID()
			items = append(items, it)
		}
	}
	if haveHost {
		for _, t := range distinctHosts(set.URLs) {
			items = append(items, domain.WorkItem{
				ID: nextID(), ScanID: scanID, Scope: domain.HostScoped, Target: t,
			})
		}
	}
	return items
}

// wellKnownItems synthesizes the r10 well-known-path sweep: for each distinct
// approved host, one URL-scoped item per fixed well-known path so the owning
// observation signatures receive their artifact (they only ever judge the
// submitted URL's response otherwise → false-clean/N-A). Each item is flagged
// WellKnown so the executor runs ONLY the observation signatures on it, never the
// URL-scoped probe adapters (see domain.WorkItem.WellKnown). They reuse the exact
// URL-scoped fetch, approval gate (SR-3), and per-host rate budget (SR-4).
//
// The sweep sits OUTSIDE the user's MaxURLs ceiling by design (Q6=A/PR-W3), so it
// carries its OWN bound: DisableWellKnown turns it off, and MaxWellKnownHosts caps
// how many distinct hosts it runs on (hosts beyond the cap are skipped and
// logged, never silently dropped). coverage accounting keys off checker
// ownership, not item count, so this leaves the 26-partition fold untouched
// (PR-W6). IDs are assigned by the caller after interleaving.
func wellKnownItems(scanID domain.ScanID, set ingest.URLSet) []domain.WorkItem {
	if set.Options.DisableWellKnown {
		return nil
	}
	hosts := distinctHosts(set.URLs)
	if limit := set.Options.MaxWellKnownHosts; limit > 0 && len(hosts) > limit {
		log.Printf("scan %s: well-known sweep capped at %d of %d hosts (MaxWellKnownHosts); %d host(s) not swept",
			scanID, limit, len(hosts), len(hosts)-limit)
		hosts = hosts[:limit]
	}
	var out []domain.WorkItem
	for _, h := range hosts {
		// Preserve the authority (host:port) from the representative URL so a
		// well-known GET hits the same port the user submitted — using the bare
		// Host would drop a non-default port and probe :80 instead. Target.Host
		// stays the hostname (the rate-limiter key + approval-gate key).
		authority := h.Host
		if u, err := url.Parse(h.URL); err == nil && u.Host != "" {
			authority = u.Host
		}
		base := h.Scheme + "://" + authority
		for _, p := range observation.WellKnownPaths() {
			out = append(out, domain.WorkItem{
				ScanID: scanID, Scope: domain.URLScoped, WellKnown: true,
				Target: domain.Target{Host: h.Host, Scheme: h.Scheme, URL: base + p},
			})
		}
	}
	return out
}

// interleaveItemsByHost round-robins work items across their target hosts so the
// sequence alternates hosts as much as possible (order-stable within a host).
// Applying this to the FULL URL-scoped set (submitted + well-known) keeps the
// PR-3a interleave invariant intact once the well-known sweep is folded in.
func interleaveItemsByHost(in []domain.WorkItem) []domain.WorkItem {
	order := []string{}
	byHost := map[string][]domain.WorkItem{}
	for _, it := range in {
		h := it.Target.Host
		if _, seen := byHost[h]; !seen {
			order = append(order, h)
		}
		byHost[h] = append(byHost[h], it)
	}
	out := make([]domain.WorkItem, 0, len(in))
	for {
		progressed := false
		for _, h := range order {
			q := byHost[h]
			if len(q) == 0 {
				continue
			}
			out = append(out, q[0])
			byHost[h] = q[1:]
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return out
}

// distinctHosts returns one representative Target per host (first-seen order) for
// host-scoped work items.
func distinctHosts(targets []domain.Target) []domain.Target {
	seen := map[string]bool{}
	var out []domain.Target
	for _, t := range targets {
		if seen[t.Host] {
			continue
		}
		seen[t.Host] = true
		out = append(out, domain.Target{Host: t.Host, Scheme: t.Scheme, URL: t.URL})
	}
	return out
}
