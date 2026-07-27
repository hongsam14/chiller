package orchestrator

import (
	"context"
	"log"
	"strings"

	"github.com/hongsam14/chiller/internal/domain"
	"github.com/hongsam14/chiller/internal/findings/coverage"
	"github.com/hongsam14/chiller/internal/platform"
)

// finalize runs once all WorkItems of a scan are terminal (US-014..021): it
// assembles the findings, folds the outcomes into a coverage ScanOutcome
// (PR-5), accounts the 26-item coverage, builds the report, persists it, and
// CAS-transitions running→completed. The terminal CAS is the durable
// single-finalizer guard for the Infra (multi-process) case.
func (o *Orchestrator) finalize(ctx context.Context, id domain.ScanID) {
	outcomes, err := o.cfg.Store.AllOutcomes(ctx, id)
	if err != nil {
		o.fail(ctx, id)
		return
	}

	// Findings axis: checkers already emit validated domain.Findings, so the
	// pipeline here is dedup (CWE-normalize + collapse + bounded evidence) → store.
	var raw []domain.Finding
	for _, oc := range outcomes {
		raw = append(raw, oc.Findings...)
	}
	findings := o.cfg.Deduper.Dedup(raw)
	if err := o.cfg.FindingsStore.SaveFindings(ctx, id, findings); err != nil {
		o.fail(ctx, id)
		return
	}

	// Coverage axis: fold per-item outcomes into a per-checker ScanOutcome, then
	// account against the fixed spec (result satisfies Coverage.Validate).
	so := o.foldOutcomes(id, ctx, outcomes)
	cov := o.cfg.Accountant.Account(so)

	rep := o.cfg.ReportBuilder.Build(id, findings, cov, o.cfg.Frameworks)
	if err := o.cfg.Store.SaveReport(ctx, id, rep); err != nil {
		o.fail(ctx, id)
		return
	}
	// Only now flip to completed: a reader that saw completed can always load the
	// report.
	won, _ := o.cfg.Store.CompareAndSetState(ctx, id, platform.StateRunning, platform.StateCompleted)

	// Everything above is idempotent — two processes finalizing the same scan write
	// the same findings and the same report. Purging the ledger is NOT: it destroys
	// the evidence, and a finalize that read the ledger afterwards would re-save the
	// same findings with their evidence stripped, overwriting the good documents.
	//
	// So only the process that WON the terminal CAS purges. It is the last writer by
	// construction: every other finalize either already ran its save (with the
	// evidence still intact, since it read the ledger before this point) or will lose
	// the CAS and stop here. maybeFinalize's guard is per-process and cannot do this
	// job — the worker pool spans pods.
	//
	// A failed purge costs a redundant copy, not correctness, so it is counted and
	// logged rather than failing an otherwise-good scan.
	if won {
		if err := o.cfg.Store.PurgeOutcomeEvidence(ctx, id); err != nil {
			o.cfg.Metrics.LedgerPurgeErrors.Add(1)
			log.Printf("finalize: scan %s: could not purge ledger evidence: %v", id, err)
		}
	}

	o.wipeCache(ctx, id)
}

// wipeCache discards the scan's cached responses (FR-12/NFR-16). Every terminal
// path calls it — completed, failed and stopped — because a scan that ends badly
// leaves exactly the same response bodies behind as one that ends well.
//
// A failure here does not affect the scan's result, so it is counted and logged
// rather than propagated. The cache entries also carry a TTL, which is what
// actually guarantees the bodies go away: if this process dies before it gets
// here, expiry still collects them.
func (o *Orchestrator) wipeCache(ctx context.Context, id domain.ScanID) {
	if o.cfg.Cache == nil {
		return
	}
	if err := o.cfg.Cache.WipeScan(ctx, id); err != nil {
		o.cfg.Metrics.CacheWipeErrors.Add(1)
		log.Printf("scan %s: could not wipe response cache: %v", id, err)
		return
	}
	o.cfg.Metrics.CacheWipes.Add(1)
}

// foldOutcomes implements PR-5: it reconstructs, per owning checker, whether the
// checker ran applicably-ok anywhere (→ RanOK/Checked), was not-applicable
// everywhere it ran (→ NotApplicable), or only ever failed (→ ToolFailed via the
// absence of RanOK). It uses the orchestrator's own scope split to know which
// checkers ran on each item, since a WorkItemOutcome does not enumerate its
// ok checkers.
func (o *Orchestrator) foldOutcomes(id domain.ScanID, ctx context.Context, outcomes []domain.WorkItemOutcome) coverage.ScanOutcome {
	// Map each item id → the item, so we know which checkers actually ran on it.
	// Scope alone is not enough: a well-known item is URL-scoped but runs the
	// observation signatures ONLY (see exec), so crediting the probes as "ran ok"
	// on it would falsely mark their PS-items Checked — the exact honesty failure
	// this fold exists to prevent.
	itemByID := o.itemsByID(ctx, id)

	// The N/A projection must use the accounting framework so its PS-IDs line up
	// with the coverage. Default KISA when no accountant is wired (fold-only tests).
	fw := domain.FrameworkKISA
	if o.cfg.Accountant != nil {
		fw = o.cfg.Accountant.Framework()
	}

	appliedOK := map[domain.CheckerID]bool{}
	failedOnly := map[domain.CheckerID]bool{}
	naCWEs := map[domain.CheckerID]map[domain.CWE]bool{}

	for _, oc := range outcomes {
		ran := o.ranCheckers(itemByID[oc.ID])
		failed := map[domain.CheckerID]bool{}
		for _, c := range oc.Failed {
			failed[c] = true
			failedOnly[c] = true
		}
		for _, c := range ran {
			cid := c.ID()
			if failed[cid] {
				continue
			}
			if cwes, isNA := oc.NotApplicable[cid]; isNA {
				if naCWEs[cid] == nil {
					naCWEs[cid] = map[domain.CWE]bool{}
				}
				for _, w := range cwes {
					naCWEs[cid][w] = true
				}
				continue
			}
			appliedOK[cid] = true // ran, neither failed nor N/A → applicable ok
		}
	}

	so := coverage.ScanOutcome{
		RanOK:         map[domain.CheckerID]bool{},
		Failed:        map[domain.CheckerID]bool{},
		NotApplicable: map[domain.CheckerID]map[domain.PSID]bool{},
	}
	for cid := range appliedOK {
		so.RanOK[cid] = true // applicable-ok anywhere wins → Checked (PR-5)
	}
	for cid := range failedOnly {
		if !appliedOK[cid] {
			so.Failed[cid] = true
		}
	}
	// N/A only for checkers that never applied-ok: project their N/A CWEs to PS-IDs.
	for cid, cwes := range naCWEs {
		if appliedOK[cid] {
			continue
		}
		for w := range cwes {
			for _, item := range o.cfg.Mapper.Project([]domain.CWE{w}, fw) {
				if so.NotApplicable[cid] == nil {
					so.NotApplicable[cid] = map[domain.PSID]bool{}
				}
				so.NotApplicable[cid][domain.PSID(item)] = true
			}
		}
	}
	return so
}

// itemsByID returns each WorkItem id → the item, from the store (the source of
// truth for the ledger). The whole item (not just its scope) is needed because
// the ran-checker set depends on the WellKnown flag too.
func (o *Orchestrator) itemsByID(ctx context.Context, id domain.ScanID) map[domain.WorkItemID]domain.WorkItem {
	items, err := o.cfg.Store.AllItems(ctx, id)
	if err != nil {
		return map[domain.WorkItemID]domain.WorkItem{}
	}
	out := make(map[domain.WorkItemID]domain.WorkItem, len(items))
	for _, it := range items {
		out[it.ID] = it
	}
	return out
}

// ranCheckers returns the checkers that actually ran on an item — it MUST mirror
// exec's split exactly, since the fold reconstructs per-checker outcome from which
// checkers ran on which item. URL-scoped items run every URL-scoped checker;
// a well-known URL-scoped item runs the observation signatures only (exec drops
// the probe adapters on it); host-scoped items run every host-scoped checker.
//
// An invalid scope returns NO checkers rather than falling through to the
// host-scoped set. This is the fold, so a wrong answer here becomes a wrong
// compliance record: attributing the host-scoped checkers to an item they never
// ran on would mark testssl/dirhunt/subzy's PS items as checked.
func (o *Orchestrator) ranCheckers(wi domain.WorkItem) []domain.Checker {
	switch wi.Scope {
	case domain.URLScoped:
		if wi.WellKnown {
			return observationCheckers(o.urlScoped)
		}
		return o.urlScoped
	case domain.HostScoped:
		return o.hostScoped
	default:
		return nil
	}
}

// observationCheckers filters to the in-process observation signatures, mirroring
// exec's well-known checker set (checker ids "observation:<CWE>" vs "probe:<tool>").
func observationCheckers(cs []domain.Checker) []domain.Checker {
	out := make([]domain.Checker, 0, len(cs))
	for _, c := range cs {
		if strings.HasPrefix(string(c.ID()), "observation:") {
			out = append(out, c)
		}
	}
	return out
}

// fail transitions a scan to failed (unrecoverable finalize error, PR-6/E-2).
func (o *Orchestrator) fail(ctx context.Context, id domain.ScanID) {
	_, _ = o.cfg.Store.CompareAndSetState(ctx, id, platform.StateRunning, platform.StateFailed)
	o.wipeCache(ctx, id)
}
