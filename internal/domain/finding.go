package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// fieldSep separates the components of an identity key. Unit-separator (0x1f)
// never appears in a CWE, URL, or tool name, so keys cannot collide.
const fieldSep = "\x1f"

// MaxEvidenceSnippets bounds the number of snippets retained per finding — one
// policy shared by every stage that produces or merges evidence (normalize,
// dedup) so the two bounds cannot silently diverge (R9, FR-12).
const MaxEvidenceSnippets = 8

// Evidence is the bounded justification for a finding. Full response bodies are
// never stored here (FR-12); snippets are bounded and redacted before logging or
// persistence (R9, SECURITY-03).
type Evidence struct {
	Snippets []string
}

// Finding is the unit a checker emits — keyed by CWE and framework-agnostic
// (US-014, NFR-03). It carries no PS-ID; PS-ID appears only on Coverage.
//
// Identity (R2, Q1=B) is (normalized CWEs, TargetURL, SourceTool): dedup is
// tool-scoped. Cross-tool duplicates are kept distinct (source attribution
// preserved); FR-06 "no double-judging across checkers" holds structurally
// because each PS item has exactly one owning checker (DQ1).
type Finding struct {
	CWEs       []CWE    // >=1; may be exactly [NoCanonicalCWE]. Store normalized (see NormalizeCWEs).
	TargetURL  string   // the URL/target this finding is about
	SourceTool string   // "observation:CWE-615" | "humble" | "httpx" | "testssl" | ...
	Severity   Severity // must be Valid (R4)
	Evidence   Evidence // bounded (FR-12)
}

// NormalizeCWEs implements rule R1: deduplicate and sort ascending by numeric
// CWE id, and enforce NoCanonicalCWE exclusivity — the sentinel is never mixed
// with real CWEs. If any real CWE is present, the sentinel is dropped (a real
// CWE means a canonical CWE exists). If only the sentinel is present, the result
// is exactly [NoCanonicalCWE]. Empty input yields an empty slice.
//
// The result is a fresh slice; the input is not mutated.
func NormalizeCWEs(in []CWE) []CWE {
	seen := make(map[CWE]struct{}, len(in))
	real := make([]CWE, 0, len(in))
	hasSentinel := false
	for _, c := range in {
		if c == NoCanonicalCWE {
			hasSentinel = true
			continue
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		real = append(real, c)
	}

	if len(real) == 0 {
		if hasSentinel {
			return []CWE{NoCanonicalCWE}
		}
		return []CWE{}
	}

	sort.Slice(real, func(i, j int) bool {
		ni, oki := real[i].numericID()
		nj, okj := real[j].numericID()
		switch {
		case oki && okj:
			return ni < nj
		case oki != okj:
			return oki // canonical "CWE-N" forms sort before any non-canonical form
		default:
			return real[i] < real[j] // stable, deterministic fallback
		}
	})
	return real
}

// IdentityKey returns the dedup key (R2). It normalizes the finding's CWEs so
// the key is stable regardless of the order the checker emitted them in.
func (f Finding) IdentityKey() string {
	norm := NormalizeCWEs(f.CWEs)
	parts := make([]string, 0, len(norm)+2)
	for _, c := range norm {
		parts = append(parts, string(c))
	}
	parts = append(parts, f.TargetURL, f.SourceTool)
	return strings.Join(parts, fieldSep)
}

// Validate enforces the Finding contract (the counterpart to Coverage.Validate):
//   - at least one CWE (a real CWE or exactly the NoCanonicalCWE sentinel),
//   - the sentinel is never mixed with real CWEs (R1),
//   - a valid Severity (R4),
//   - non-empty TargetURL and SourceTool.
//
// It validates the finding as normalized; callers that build findings by hand
// should store NormalizeCWEs(f.CWEs). Returns nil when well-formed.
func (f Finding) Validate() error {
	norm := NormalizeCWEs(f.CWEs)
	if len(norm) == 0 {
		return errors.New("finding has no CWEs (need >=1 real CWE or NoCanonicalCWE)")
	}
	if len(norm) > 1 {
		for _, c := range norm {
			if c == NoCanonicalCWE {
				return errors.New("finding mixes NoCanonicalCWE with real CWEs")
			}
		}
	}
	if !f.Severity.Valid() {
		return fmt.Errorf("finding has invalid severity %d", f.Severity)
	}
	if f.TargetURL == "" {
		return errors.New("finding has empty TargetURL")
	}
	if f.SourceTool == "" {
		return errors.New("finding has empty SourceTool")
	}
	return nil
}
