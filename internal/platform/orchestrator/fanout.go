package orchestrator

import (
	"fmt"

	"github.com/hongsam14/chiller/internal/domain"
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
		for _, t := range interleaveByHost(set.URLs) {
			items = append(items, domain.WorkItem{
				ID: nextID(), ScanID: scanID, Scope: domain.URLScoped, Target: t,
			})
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

// interleaveByHost round-robins targets across their hosts so the sequence
// alternates hosts as much as possible (order-stable within a host).
func interleaveByHost(targets []domain.Target) []domain.Target {
	order := []string{}
	byHost := map[string][]domain.Target{}
	for _, t := range targets {
		if _, seen := byHost[t.Host]; !seen {
			order = append(order, t.Host)
		}
		byHost[t.Host] = append(byHost[t.Host], t)
	}
	out := make([]domain.Target, 0, len(targets))
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
