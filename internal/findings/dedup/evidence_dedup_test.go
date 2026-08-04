package dedup

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

// r20 property tests for structured-evidence dedup.

func mkItem(i int) domain.EvidenceItem {
	return domain.EvidenceItem{
		Snippet:  "snippet-" + string(rune('a'+i%26)),
		Location: domain.Location{Part: domain.PartBody, Offset: i},
		Rule:     "observation:CWE-615/r",
	}
}

// randFindings builds findings drawn from a small identity space so collisions (and
// thus merges) are frequent, each carrying a random number of evidence items.
func randFindings(rng *rand.Rand, n int) []domain.Finding {
	urls := []string{"https://a/", "https://b/"}
	tools := []string{"observation:CWE-615", "humble"}
	out := make([]domain.Finding, 0, n)
	for range n {
		nItems := rng.Intn(6)
		items := make([]domain.EvidenceItem, 0, nItems)
		for j := range nItems {
			items = append(items, mkItem(rng.Intn(12)+j))
		}
		f := domain.Finding{
			CWEs:       []domain.CWE{"CWE-615"},
			TargetURL:  urls[rng.Intn(len(urls))],
			SourceTool: tools[rng.Intn(len(tools))],
			Severity:   domain.SevMedium,
			Evidence:   domain.Evidence{Items: items},
		}
		if rng.Intn(2) == 0 {
			f.Evidence.Request = &domain.RequestContext{Method: "GET", URL: f.TargetURL}
		}
		out = append(out, f)
	}
	return out
}

// Property: dedup never exceeds the item bound, and is idempotent (P2 preserved).
func TestProperty_DedupBoundedAndIdempotent(t *testing.T) {
	d := New()
	rng := rand.New(rand.NewSource(1))
	for iter := range 300 {
		in := randFindings(rng, rng.Intn(20))
		once := d.Dedup(in)
		for _, f := range once {
			if len(f.Evidence.Items) > domain.MaxEvidenceItems {
				t.Fatalf("iter %d: %d items exceeds bound %d", iter, len(f.Evidence.Items), domain.MaxEvidenceItems)
			}
		}
		twice := d.Dedup(once)
		if !reflect.DeepEqual(once, twice) {
			t.Fatalf("iter %d: dedup not idempotent", iter)
		}
	}
}

// Property: merging two same-identity findings unions their items by item identity
// (no duplicate items) and keeps the first non-nil request context.
func TestDedup_MergesItemsAndKeepsContext(t *testing.T) {
	base := func(items []domain.EvidenceItem, req *domain.RequestContext) domain.Finding {
		return domain.Finding{
			CWEs: []domain.CWE{"CWE-615"}, TargetURL: "https://h/", SourceTool: "observation:CWE-615",
			Severity: domain.SevLow, Evidence: domain.Evidence{Items: items, Request: req},
		}
	}
	a := base([]domain.EvidenceItem{mkItem(1), mkItem(2)}, &domain.RequestContext{Method: "GET", URL: "https://h/"})
	b := base([]domain.EvidenceItem{mkItem(2), mkItem(3)}, &domain.RequestContext{Method: "GET", URL: "https://OTHER/"})
	b.Severity = domain.SevHigh

	got := New().Dedup([]domain.Finding{a, b})
	if len(got) != 1 {
		t.Fatalf("want 1 merged finding, got %d", len(got))
	}
	g := got[0]
	if g.Severity != domain.SevHigh {
		t.Errorf("severity should be max (High), got %v", g.Severity)
	}
	if len(g.Evidence.Items) != 3 { // {1,2} ∪ {2,3} = {1,2,3}
		t.Errorf("want 3 unioned items, got %d", len(g.Evidence.Items))
	}
	if g.Evidence.Request == nil || g.Evidence.Request.URL != "https://h/" {
		t.Errorf("should keep the first non-nil request context, got %#v", g.Evidence.Request)
	}
}
