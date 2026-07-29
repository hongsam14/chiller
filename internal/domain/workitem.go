package domain

// WorkItem is the fan-out unit (C-6). A Scan(Task) aggregate fans out into many
// WorkItems distributed via the WorkQueue; a URL-scoped WorkItem is one URL run
// through all URL-scoped checkers (one shared fetch, FR-02), a host-scoped
// WorkItem is one host through one host-scoped checker. The completion ledger
// over WorkItems (in the Task store) is the source of truth for CAS-resume (FR-07).
type WorkItem struct {
	ID     WorkItemID `bson:"id"`
	ScanID ScanID     `bson:"scanId"`
	Scope  ScopeKind  `bson:"scope"`
	Target Target     `bson:"target"`
	// WellKnown marks an engine-synthesized well-known-path item (r10). Such an
	// item exists only to deliver its artifact to the in-process observation
	// signatures, so the executor runs ONLY those on it — never the external-CLI
	// probe adapters, which would otherwise be re-exec'd once per synthesized path
	// against paths that are almost always 404 (amplification). It is a data field,
	// so it round-trips through the bson ledger and json queue payload unchanged;
	// old items decode it as false (a normal, all-checkers URL item).
	WellKnown bool `bson:"wellKnown,omitempty" json:"wellKnown,omitempty"`
	// WellKnownCWE is the CWE of the ONE signature that owns this synthesized
	// well-known path (r16, FR-6): the executor runs only that observation signature
	// on the item, not the whole set, so path-agnostic scanners (PS-009/023/cookies)
	// no longer fire ~14× per host on the sweep. Set together with WellKnown by the
	// fan-out (the only producer). Empty on old items → falls back to WellKnown.
	WellKnownCWE CWE `bson:"wellKnownCwe,omitempty" json:"wellKnownCwe,omitempty"`
}

// WorkItemOutcome reports what a WorkItem produced. Failed lists the checkers
// that errored on this item; their owned PS-IDs become StatusToolFailed in the
// coverage accounting (US-027, R7). NotApplicable records which CWEs each checker
// judged structurally inapplicable (FQ5) — the accountant maps those to N/A items.
type WorkItemOutcome struct {
	ID            WorkItemID          `bson:"id"`
	Findings      []Finding           `bson:"findings"`
	Failed        []CheckerID         `bson:"failed"`
	NotApplicable map[CheckerID][]CWE `bson:"notApplicable"`
	// TruncatedURLs lists the exact URLs whose fetched body (the main response or a
	// discovered first-party JS asset) was not fully available as decoded text (r17,
	// FR-T4). It rides the ledger so finalize can surface the affected URLs in the
	// report — the JS asset's own URL, not the page's, when only the JS was truncated.
	TruncatedURLs []string `bson:"truncatedUrls,omitempty"`
}
