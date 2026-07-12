package orchestrator

import (
	"context"

	"github.com/enki-offen/chiller/internal/domain"
	"github.com/enki-offen/chiller/internal/findings/coverage"
	"github.com/enki-offen/chiller/internal/platform"
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
	if err := o.cfg.FindingsStore.SaveFindings(id, findings); err != nil {
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
	// report. Losing this CAS (another process finalized) is a harmless no-op.
	_, _ = o.cfg.Store.CompareAndSetState(ctx, id, platform.StateRunning, platform.StateCompleted)
}

// foldOutcomes implements PR-5: it reconstructs, per owning checker, whether the
// checker ran applicably-ok anywhere (→ RanOK/Checked), was not-applicable
// everywhere it ran (→ NotApplicable), or only ever failed (→ ToolFailed via the
// absence of RanOK). It uses the orchestrator's own scope split to know which
// checkers ran on each item, since a WorkItemOutcome does not enumerate its
// ok checkers.
func (o *Orchestrator) foldOutcomes(id domain.ScanID, ctx context.Context, outcomes []domain.WorkItemOutcome) coverage.ScanOutcome {
	// Map each item id → its scope so we know which checkers ran on it.
	scopeOf := o.itemScopes(ctx, id)

	appliedOK := map[domain.CheckerID]bool{}
	failedOnly := map[domain.CheckerID]bool{}
	naCWEs := map[domain.CheckerID]map[domain.CWE]bool{}

	for _, oc := range outcomes {
		ran := o.checkersForScope(scopeOf[oc.ID])
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
			for _, item := range o.cfg.Mapper.Project([]domain.CWE{w}, domain.FrameworkKISA) {
				if so.NotApplicable[cid] == nil {
					so.NotApplicable[cid] = map[domain.PSID]bool{}
				}
				so.NotApplicable[cid][domain.PSID(item)] = true
			}
		}
	}
	return so
}

// itemScopes returns each WorkItem id → its scope, from the store (the source of
// truth for the ledger).
func (o *Orchestrator) itemScopes(ctx context.Context, id domain.ScanID) map[domain.WorkItemID]domain.ScopeKind {
	items, err := o.cfg.Store.AllItems(ctx, id)
	if err != nil {
		return map[domain.WorkItemID]domain.ScopeKind{}
	}
	out := make(map[domain.WorkItemID]domain.ScopeKind, len(items))
	for _, it := range items {
		out[it.ID] = it.Scope
	}
	return out
}

// checkersForScope returns the checkers that run on an item of the given scope
// (the same split exec uses): URL-scoped items run every URL-scoped checker;
// host-scoped items run every host-scoped checker.
func (o *Orchestrator) checkersForScope(s domain.ScopeKind) []domain.Checker {
	if s == domain.URLScoped {
		return o.urlScoped
	}
	return o.hostScoped
}

// fail transitions a scan to failed (unrecoverable finalize error, PR-6/E-2).
func (o *Orchestrator) fail(ctx context.Context, id domain.ScanID) {
	_, _ = o.cfg.Store.CompareAndSetState(ctx, id, platform.StateRunning, platform.StateFailed)
}
