package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// fieldSep separates the components of an identity key. Unit-separator (0x1f)
// never appears in a CWE, URL, or tool name, so keys cannot collide.
const fieldSep = "\x1f"

// MaxEvidenceItems bounds the number of evidence items retained per finding — one
// policy shared by every stage that produces or merges evidence (normalize,
// dedup) so the two bounds cannot silently diverge (R9, FR-12). (Formerly
// MaxEvidenceSnippets; the bound is unchanged — an "item" carries one snippet.)
const MaxEvidenceItems = 8

// MaxEvidenceHeaders bounds the request/response header lists carried in evidence
// context, and MaxEvidenceHeaderValueLen bounds a single header value — so richer
// evidence (r20) can never become an unbounded header dump (FR-E8, SECURITY-03).
const (
	MaxEvidenceHeaders        = 12
	MaxEvidenceHeaderValueLen = 256
)

// Evidence is the bounded, structured justification for a finding (r20). Full
// response bodies are never stored (FR-12); every string is bounded and redacted
// before logging or persistence (R9, SECURITY-03), and the whole struct is
// encrypted at rest (SECURITY-01). It is the machine-readable feed a downstream
// consumer (e.g. an AI active-attack planner / false-positive checker) uses to
// judge and reproduce a finding.
type Evidence struct {
	Items []EvidenceItem `bson:"items,omitempty" json:"items,omitempty"` // individual matches, <= MaxEvidenceItems
	// Request/Response describe the HTTP exchange the match actually came from — NOT
	// necessarily the finding's TargetURL. A match found in a discovered JS file the
	// page delivered carries that JS exchange here and its URL in Location.SourceURL.
	// probe findings usually leave these nil (best-effort, no HAR available, FR-E7).
	Request  *RequestContext  `bson:"request,omitempty" json:"request,omitempty"`
	Response *ResponseContext `bson:"response,omitempty" json:"response,omitempty"`
}

// Empty reports whether the evidence carries nothing to persist. Items AND both
// contexts must be empty — so evidence that is only request/response context is
// still sealed and survives a seal→open round trip (r20 seal-identity property).
func (e Evidence) Empty() bool {
	return len(e.Items) == 0 && e.Request == nil && e.Response == nil
}

// SnippetStrings returns each item's snippet, in order — the plain-text view of the
// evidence (for logging/summaries and callers that only want the matched content).
func (e Evidence) SnippetStrings() []string {
	if len(e.Items) == 0 {
		return nil
	}
	out := make([]string, len(e.Items))
	for i, it := range e.Items {
		out[i] = it.Snippet
	}
	return out
}

// EvidenceOf builds evidence from bare snippet strings — for producers that only
// have matched text (no location/context) and for tests. Structured capture
// (location, rule, request/response) is set by the checkers directly.
func EvidenceOf(snippets ...string) Evidence {
	if len(snippets) == 0 {
		return Evidence{}
	}
	items := make([]EvidenceItem, len(snippets))
	for i, s := range snippets {
		items[i] = EvidenceItem{Snippet: s}
	}
	return Evidence{Items: items}
}

// EvidenceItem is one matched piece of evidence.
type EvidenceItem struct {
	Snippet    string   `bson:"snippet" json:"snippet"`                           // the actual matched content, bounded + redacted
	Location   Location `bson:"location,omitempty" json:"location,omitempty"`     // where the match sits (FR-E2)
	Rule       string   `bson:"rule,omitempty" json:"rule,omitempty"`             // firing signature/rule id (FR-E3)
	Hypothesis string   `bson:"hypothesis,omitempty" json:"hypothesis,omitempty"` // what the rule detects — the AI's starting hypothesis (FR-E3)
}

// MatchPart names the part of the HTTP exchange a match sits in.
type MatchPart string

const (
	PartHeader MatchPart = "header"
	PartBody   MatchPart = "body"
	PartCookie MatchPart = "cookie"
	PartURL    MatchPart = "url"
)

// Location says where in the exchange a match sits, so a consumer can separate the
// matched token from its surrounding context and re-fetch it. Offset/MatchLen index
// the ORIGINAL (pre-whitespace-collapse) body of the SourceURL exchange when set,
// else the finding's primary response body; they are meaningful only for PartBody.
type Location struct {
	Part      MatchPart `bson:"part,omitempty" json:"part,omitempty"`
	Name      string    `bson:"name,omitempty" json:"name,omitempty"`           // header/cookie name (PartHeader|PartCookie)
	Offset    int       `bson:"offset,omitempty" json:"offset,omitempty"`       // byte offset in the body (PartBody)
	MatchLen  int       `bson:"matchLen,omitempty" json:"matchLen,omitempty"`   // matched length in bytes at Offset (PartBody)
	SourceURL string    `bson:"sourceUrl,omitempty" json:"sourceUrl,omitempty"` // set only when the match came from a URL != the finding's TargetURL (e.g. discovered JS)
}

// RequestContext is the reproduction/pivot entry point for a downstream consumer
// (FR-E4). The passive fetcher always issues a plain GET with a fixed minimal
// header set, so Headers is normally omitted (nothing informative to record) —
// Method+URL is the reproduction key; omission is honest, never fabricated (D5).
type RequestContext struct {
	Method  string   `bson:"method,omitempty" json:"method,omitempty"`
	URL     string   `bson:"url,omitempty" json:"url,omitempty"`
	Headers []Header `bson:"headers,omitempty" json:"headers,omitempty"`
}

// ResponseContext is the false-positive/reproduction context for a match (FR-E5).
// Headers is a bounded, deterministic allowlist — never the full header set.
type ResponseContext struct {
	Status      int      `bson:"status,omitempty" json:"status,omitempty"`
	ContentType string   `bson:"contentType,omitempty" json:"contentType,omitempty"`
	Headers     []Header `bson:"headers,omitempty" json:"headers,omitempty"`
	Truncated   bool     `bson:"truncated,omitempty" json:"truncated,omitempty"`
}

// Finding is the unit a checker emits — keyed by CWE and framework-agnostic
// (US-014, NFR-03). It carries no PS-ID; PS-ID appears only on Coverage.
//
// Identity (R2, Q1=B) is (normalized CWEs, TargetURL, SourceTool): dedup is
// tool-scoped. Cross-tool duplicates are kept distinct (source attribution
// preserved); FR-06 "no double-judging across checkers" holds structurally
// because each PS item has exactly one owning checker (DQ1).
type Finding struct {
	CWEs       []CWE    `bson:"cwes" json:"cwes"`             // >=1; may be exactly [NoCanonicalCWE]. Store normalized (see NormalizeCWEs).
	TargetURL  string   `bson:"targetUrl" json:"targetUrl"`   // the URL/target this finding is about
	SourceTool string   `bson:"sourceTool" json:"sourceTool"` // "observation:CWE-615" | "humble" | "httpx" | "testssl" | ...
	Severity   Severity `bson:"severity" json:"severity"`     // must be Valid (R4)
	Evidence   Evidence `bson:"evidence" json:"evidence"`     // bounded (FR-12); encrypted at rest (SECURITY-01)
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

// FindingKey is a finding's stable, encoding-safe identity: the hex SHA-256 of
// IdentityKey. A report's framework labels reference findings by this key rather
// than by their position in the Findings slice, so a report stays correct no
// matter what order a store returns findings in (C2=B). IdentityKey itself is
// unsuitable as a wire value — it joins its parts with a 0x1f control character.
type FindingKey string

// Key returns the finding's FindingKey. Like IdentityKey it normalizes the CWEs
// first, so it does not depend on the order the checker emitted them in.
func (f Finding) Key() FindingKey {
	sum := sha256.Sum256([]byte(f.IdentityKey()))
	return FindingKey(hex.EncodeToString(sum[:]))
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
