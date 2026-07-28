// Package exec runs one WorkItem through the checkers and aggregates the outcome
// (US-027, SR-8/9). It owns the Fetch+Cache phase for URL-scoped items (①fetch
// ②discover ③fetch JS ④ScanContext) and per-checker fault isolation.
package exec

import (
	"context"
	"strings"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/scan/fetch"
	"github.com/hongsam14/chiller/internal/scan/safety"
)

// Executor runs a WorkItem. The platform schedules; scan executes. ctx carries
// the scan-level deadline/cancellation into every outbound fetch (SR-8).
type Executor interface {
	Run(ctx context.Context, wi domain.WorkItem, cache fetch.ResponseCache) domain.WorkItemOutcome
}

type executor struct {
	fetcher    fetch.Fetcher
	disc       fetch.Discoverer
	gate       safety.ApprovalGate
	urlScoped  []domain.Checker
	hostScoped []domain.Checker
}

// New builds an Executor over a set of checkers, splitting them by scope (C-6).
// gate authorizes host-scoped probes (which do their own I/O) before they run,
// so the fail-closed approval invariant holds on the host path too, not only the
// URL fetch path (SR-3).
func New(checkers []domain.Checker, f fetch.Fetcher, d fetch.Discoverer, gate safety.ApprovalGate) Executor {
	e := &executor{fetcher: f, disc: d, gate: gate}
	for _, c := range checkers {
		if c.Scope() == domain.URLScoped {
			e.urlScoped = append(e.urlScoped, c)
		} else {
			e.hostScoped = append(e.hostScoped, c)
		}
	}
	return e
}

func (e *executor) Run(ctx context.Context, wi domain.WorkItem, cache fetch.ResponseCache) domain.WorkItemOutcome {
	out := domain.WorkItemOutcome{ID: wi.ID, NotApplicable: map[domain.CheckerID][]domain.CWE{}}

	var sctx domain.ScanContext
	var checkers []domain.Checker

	// An item whose scope did not survive storage is NOT quietly treated as one of
	// the two real scopes: guessing would run the wrong checker set and then let the
	// coverage fold record the checkers that never ran as having checked their items.
	// Fail the item instead — a tool-failed item is visible in the report, a
	// mis-scoped one is not.
	if !wi.Scope.Valid() {
		for _, c := range append(append([]domain.Checker{}, e.urlScoped...), e.hostScoped...) {
			out.Failed = append(out.Failed, c.ID())
		}
		return out
	}

	if wi.Scope == domain.URLScoped {
		html, err := e.fetcher.Fetch(ctx, wi.ScanID, wi.Target) // ①
		if err != nil {
			// fetch failed ⇒ no URL-scoped checker can run → all tool-failed (SR-8)
			for _, c := range e.urlScoped {
				out.Failed = append(out.Failed, c.ID())
			}
			return out
		}
		var js []*domain.HAREntry
		for _, jt := range e.disc.Discover(wi.Target, html) { // ②
			if je, ferr := e.fetcher.Fetch(ctx, wi.ScanID, jt); ferr == nil { // ③ (gated inside Fetch)
				js = append(js, je)
			}
		}
		sctx = domain.ScanContext{Target: wi.Target, Response: html, DiscoveredJS: js} // ④
		checkers = e.urlScoped
		// An engine-synthesized well-known item (r10) is a delivery vehicle for the
		// in-process observation signatures only. Running the URL-scoped probe
		// adapters (humble/httpx/Corsy/changeme) on it too would re-exec each binary
		// once per synthesized path — ~15× per host, against paths that are almost
		// always 404 — amplifying runtime and the probes' own (un-rate-limited)
		// outbound traffic. Coverage is unaffected: the probes still run on the
		// user's submitted URL items, and the fold keys off checker ownership, not
		// item count.
		// r16 (FR-6): run ONLY the signature that OWNS this well-known path, not the
		// whole observation set — a path-agnostic scanner (PS-009 comment, PS-023
		// captcha, cookies) firing on every well-known path was the dominant FP
		// amplifier. Those still run on the submitted URLs, so coverage is unaffected.
		// Old items (no WellKnownCWE) fall back to the r10 observation-only set.
		if wi.WellKnownCWE != "" {
			checkers = observationForCWE(e.urlScoped, wi.WellKnownCWE)
		} else if wi.WellKnown {
			checkers = observationOnly(e.urlScoped)
		}
	} else {
		// host-scoped probes do their own outbound I/O — gate the host first, so
		// nothing runs against an unapproved host (fail-closed, SR-3).
		if e.gate != nil {
			if err := e.gate.Authorize(wi.Target); err != nil {
				for _, c := range e.hostScoped {
					out.Failed = append(out.Failed, c.ID())
				}
				return out
			}
		}
		sctx = domain.ScanContext{Target: wi.Target} // host-scoped: own protocol I/O, Response nil
		checkers = e.hostScoped
	}

	for _, c := range checkers { // run independently (SR-8/9)
		res, err := c.Check(sctx)
		if err != nil {
			out.Failed = append(out.Failed, c.ID())
			continue
		}
		out.Findings = append(out.Findings, res.Findings...)
		if len(res.NotApplicable) > 0 {
			out.NotApplicable[c.ID()] = res.NotApplicable
		}
	}
	return out
}

// observationOnly filters a URL-scoped checker set down to the in-process
// Observation engine, dropping the external-CLI probe adapters. Observation
// checker ids are "observation:<CWE>" and probe ids are "probe:<tool>"
// (see the registry), so the prefix is the reliable discriminator.
func observationOnly(cs []domain.Checker) []domain.Checker {
	out := make([]domain.Checker, 0, len(cs))
	for _, c := range cs {
		if strings.HasPrefix(string(c.ID()), "observation:") {
			out = append(out, c)
		}
	}
	return out
}

// observationForCWE returns the single observation signature that owns cwe — the
// checker whose id is "observation:<cwe>" (r16, FR-6). A well-known item runs only
// its owning signature.
func observationForCWE(cs []domain.Checker, cwe domain.CWE) []domain.Checker {
	want := domain.CheckerID("observation:" + string(cwe))
	for _, c := range cs {
		if c.ID() == want {
			return []domain.Checker{c}
		}
	}
	return nil
}
