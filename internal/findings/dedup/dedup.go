// Package dedup collapses duplicate findings by their tool-scoped identity
// (US-015, P2). Uses domain.Finding.IdentityKey (R2) and domain.Max (R3) — it
// does not derive its own identity/severity logic.
package dedup

import "github.com/hongsam14/chiller/internal/domain"

// Deduplicator is the seam (NFR-01).
type Deduplicator interface {
	// Dedup is idempotent and order-independent as a set (P2): dedup∘dedup=dedup.
	Dedup([]domain.Finding) []domain.Finding
}

type deduplicator struct{ maxSnippets int }

// New builds a Deduplicator.
func New() Deduplicator { return &deduplicator{maxSnippets: domain.MaxEvidenceSnippets} }

func (d *deduplicator) Dedup(in []domain.Finding) []domain.Finding {
	byKey := make(map[string]domain.Finding, len(in))
	order := make([]string, 0, len(in))
	for _, f := range in {
		nf := f
		nf.CWEs = domain.NormalizeCWEs(f.CWEs) // store normalized so re-dedup is stable
		k := nf.IdentityKey()
		if ex, ok := byKey[k]; ok {
			byKey[k] = d.collapse(ex, nf)
		} else {
			byKey[k] = nf
			order = append(order, k)
		}
	}
	out := make([]domain.Finding, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	return out
}

// collapse merges two same-identity findings (R3): max severity, evidence union
// bounded, everything else identical by construction of the key.
func (d *deduplicator) collapse(a, b domain.Finding) domain.Finding {
	a.Severity = domain.Max(a.Severity, b.Severity)
	a.Evidence.Snippets = boundUnion(a.Evidence.Snippets, b.Evidence.Snippets, d.maxSnippets)
	return a
}

// boundUnion returns the de-duplicated union of two snippet lists, truncated —
// iterating both lists in place without allocating a merged slice.
func boundUnion(x, y []string, max int) []string {
	seen := make(map[string]bool, len(x)+len(y))
	out := make([]string, 0, len(x)+len(y))
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range x {
		add(s)
	}
	for _, s := range y {
		add(s)
	}
	if len(out) > max {
		out = out[:max]
	}
	return out
}
