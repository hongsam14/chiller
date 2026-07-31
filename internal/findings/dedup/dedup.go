// Package dedup collapses duplicate findings by their tool-scoped identity
// (US-015, P2). Uses domain.Finding.IdentityKey (R2) and domain.Max (R3) — it
// does not derive its own identity/severity logic.
package dedup

import (
	"strconv"
	"strings"

	"github.com/hongsam14/chiller/internal/domain"
)

// Deduplicator is the seam (NFR-01).
type Deduplicator interface {
	// Dedup is idempotent and order-independent as a set (P2): dedup∘dedup=dedup.
	Dedup([]domain.Finding) []domain.Finding
}

type deduplicator struct{ maxItems int }

// New builds a Deduplicator.
func New() Deduplicator { return &deduplicator{maxItems: domain.MaxEvidenceItems} }

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

// collapse merges two same-identity findings (R3): max severity, evidence-item
// union bounded. Request/Response describe the same exchange (the identity key
// includes TargetURL), so the first non-nil is kept.
func (d *deduplicator) collapse(a, b domain.Finding) domain.Finding {
	a.Severity = domain.Max(a.Severity, b.Severity)
	a.Evidence.Items = boundUnionItems(a.Evidence.Items, b.Evidence.Items, d.maxItems)
	if a.Evidence.Request == nil {
		a.Evidence.Request = b.Evidence.Request
	}
	if a.Evidence.Response == nil {
		a.Evidence.Response = b.Evidence.Response
	}
	return a
}

// boundUnionItems returns the de-duplicated union of two evidence-item lists,
// truncated. Item identity is (snippet, part, name, offset, sourceURL) so the
// union is deterministic and idempotent (P2).
func boundUnionItems(x, y []domain.EvidenceItem, max int) []domain.EvidenceItem {
	seen := make(map[string]bool, len(x)+len(y))
	out := make([]domain.EvidenceItem, 0, len(x)+len(y))
	add := func(it domain.EvidenceItem) {
		k := itemKey(it)
		if !seen[k] {
			seen[k] = true
			out = append(out, it)
		}
	}
	for _, it := range x {
		add(it)
	}
	for _, it := range y {
		add(it)
	}
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// itemKey is the union-dedup identity of an evidence item. The unit-separator
// (0x1f) cannot appear in a snippet/URL/name so parts cannot collide.
func itemKey(it domain.EvidenceItem) string {
	return strings.Join([]string{
		it.Snippet,
		string(it.Location.Part),
		it.Location.Name,
		strconv.Itoa(it.Location.Offset),
		it.Location.SourceURL,
	}, "\x1f")
}
