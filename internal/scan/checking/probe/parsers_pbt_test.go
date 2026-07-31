package probe

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

// PBT-D1 (property-based-testing, FULL): across randomized VALID-schema inputs,
// every finding a probe parser emits must own exactly one CWE drawn from that
// tool's declared ownership set, carry a valid severity, name its tool, and stay
// within the evidence bound. This guards the r9-rewritten dirhunt/subzy parsers
// (whose real schemas differ from the fabricated ones) against ever emitting a
// wrong or unowned CWE, and pins the ownership → PS-ID invariant.
func TestParsers_Property_OwnedCWEOnly(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	owned := map[string]map[domain.CWE]bool{
		"subzy":      {domain.NoCanonicalCWE: true},
		"testssl.sh": {"CWE-1327": true},
		"dirhunt":    {"CWE-548": true},
	}
	check := func(tool string, p Parser, in []byte) {
		fs, err := p(probeTarget, in)
		if err != nil {
			t.Fatalf("%s: valid-schema input errored: %v (input=%s)", tool, err, in)
		}
		for _, f := range fs {
			if len(f.CWEs) != 1 || !owned[tool][f.CWEs[0]] {
				t.Fatalf("%s: finding CWEs %v not the single owned CWE (input=%s)", tool, f.CWEs, in)
			}
			if !f.Severity.Valid() {
				t.Fatalf("%s: invalid severity %v (input=%s)", tool, f.Severity, in)
			}
			if f.SourceTool != tool {
				t.Fatalf("%s: sourceTool=%q, want %q", tool, f.SourceTool, tool)
			}
			if len(f.Evidence.SnippetStrings()) > domain.MaxEvidenceItems {
				t.Fatalf("%s: evidence unbounded %d", tool, len(f.Evidence.SnippetStrings()))
			}
		}
	}

	for range 300 {
		// subzy: random array of {subdomain,status,engine}; only status=="vulnerable" flags.
		subEntries := make([]map[string]any, rng.Intn(4))
		for i := range subEntries {
			status := []string{"vulnerable", "not vulnerable", "error", ""}[rng.Intn(4)]
			subEntries[i] = map[string]any{"subdomain": fmt.Sprintf("s%d.test", i), "status": status, "engine": ""}
		}
		subJSON, _ := json.Marshal(subEntries)
		check("subzy", subzyParse, subJSON)

		// testssl: random array of {id,severity,finding}; only HIGH/CRITICAL flag.
		tEntries := make([]map[string]any, rng.Intn(6))
		for i := range tEntries {
			sev := []string{"INFO", "OK", "LOW", "MEDIUM", "HIGH", "CRITICAL"}[rng.Intn(6)]
			tEntries[i] = map[string]any{"id": fmt.Sprintf("id%d", i), "severity": sev, "finding": "x"}
		}
		tJSON, _ := json.Marshal(tEntries)
		check("testssl.sh", testsslParse, tJSON)

		// dirhunt: random index_of_processors, some with the "index_of" flag.
		idx := make([]map[string]any, rng.Intn(4))
		for i := range idx {
			flags := []any{"200"}
			if rng.Intn(2) == 0 {
				flags = append(flags, "index_of")
			}
			idx[i] = map[string]any{"crawler_url": map[string]any{"flags": flags, "url": map[string]any{"address": "http://t/x"}}}
		}
		dJSON, _ := json.Marshal(map[string]any{"index_of_processors": idx})
		check("dirhunt", dirhuntParse, dJSON)
	}
}
