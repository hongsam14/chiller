package dedup

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

func f(cwe domain.CWE, url, tool string, sev domain.Severity, snip string) domain.Finding {
	return domain.Finding{CWEs: []domain.CWE{cwe}, TargetURL: url, SourceTool: tool, Severity: sev, Evidence: domain.Evidence{Snippets: []string{snip}}}
}

func keys(fs []domain.Finding) []string {
	k := make([]string, len(fs))
	for i, x := range fs {
		k[i] = x.IdentityKey()
	}
	sort.Strings(k)
	return k
}

func TestDedup_CollapsesSameIdentity(t *testing.T) {
	d := New()
	in := []domain.Finding{
		f("CWE-540", "u", "httpx", domain.SevLow, "a"),
		f("CWE-540", "u", "httpx", domain.SevHigh, "b"), // same identity, higher severity
	}
	out := d.Dedup(in)
	if len(out) != 1 {
		t.Fatalf("want 1 collapsed, got %d", len(out))
	}
	if out[0].Severity != domain.SevHigh {
		t.Errorf("collapse should take max severity, got %v", out[0].Severity)
	}
	if len(out[0].Evidence.Snippets) != 2 {
		t.Errorf("evidence should union, got %v", out[0].Evidence.Snippets)
	}
}

func TestDedup_KeepsDistinctTools(t *testing.T) {
	d := New()
	out := d.Dedup([]domain.Finding{
		f("CWE-540", "u", "httpx", domain.SevLow, "a"),
		f("CWE-540", "u", "humble", domain.SevLow, "a"), // different tool → distinct (Q1=B)
	})
	if len(out) != 2 {
		t.Errorf("different SourceTool must stay distinct, got %d", len(out))
	}
}

// P2: idempotent + order-independent (set equality).
func TestDedup_Property_IdempotentAndCommutative(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	cwes := []domain.CWE{"CWE-540", "CWE-79", "CWE-200"}
	tools := []string{"httpx", "humble", "observation:CWE-615"}
	urls := []string{"u1", "u2"}
	for range 200 {
		n := rng.Intn(12)
		in := make([]domain.Finding, n)
		for i := range in {
			in[i] = f(cwes[rng.Intn(len(cwes))], urls[rng.Intn(len(urls))], tools[rng.Intn(len(tools))], domain.Severity(rng.Intn(5)), "s")
		}
		d := New()
		once := d.Dedup(in)
		// idempotent
		twice := d.Dedup(once)
		if !reflect.DeepEqual(once, twice) {
			t.Fatalf("not idempotent: %v vs %v", once, twice)
		}
		// commutative as a set: shuffle then dedup → same key set
		shuf := append([]domain.Finding(nil), in...)
		rng.Shuffle(len(shuf), func(i, j int) { shuf[i], shuf[j] = shuf[j], shuf[i] })
		if !reflect.DeepEqual(keys(once), keys(d.Dedup(shuf))) {
			t.Fatalf("not order-independent as a set")
		}
	}
}
