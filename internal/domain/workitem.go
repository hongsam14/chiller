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
