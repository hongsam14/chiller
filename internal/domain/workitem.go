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
}
