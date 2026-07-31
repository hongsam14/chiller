package observation

import (
	"strings"
	"testing"

	"github.com/hongsam14/chiller/internal/domain"
)

// F1 regression: a secret found in a discovered JS file (not the page body) must
// attribute the evidence's Request/Response to the JS exchange and pin the JS URL
// in Location.SourceURL — so a downstream consumer re-fetches the JS, not the page.
// The finding's TargetURL stays the page (identity unchanged).
func TestEvidence_JSSourcedMatch_AttributesToJSExchange(t *testing.T) {
	sig := commentSig(t)
	const page = "https://h/"
	const jsURL = "https://h/vendor/app.js"
	ctx := jsCtx(page, jsURL, `var c={apiKey:"`+realJWT+`"};`)

	fs, na, err := sig.Judge(ctx)
	if err != nil || na {
		t.Fatalf("Judge: err=%v na=%v", err, na)
	}
	if len(fs) != 1 {
		t.Fatalf("want 1 finding, got %d", len(fs))
	}
	f := fs[0]

	if f.TargetURL != page {
		t.Errorf("TargetURL (identity) must stay the page %q, got %q", page, f.TargetURL)
	}
	if len(f.Evidence.Items) != 1 {
		t.Fatalf("want 1 evidence item, got %d", len(f.Evidence.Items))
	}
	it := f.Evidence.Items[0]
	if it.Location.SourceURL != jsURL {
		t.Errorf("Location.SourceURL must be the JS URL %q, got %q", jsURL, it.Location.SourceURL)
	}
	if it.Location.Part != domain.PartBody {
		t.Errorf("Location.Part = %q, want body", it.Location.Part)
	}
	if !strings.HasPrefix(it.Rule, "observation:CWE-615/") {
		t.Errorf("Rule must carry the sub-rule, got %q", it.Rule)
	}
	if it.Hypothesis == "" {
		t.Error("Hypothesis must be set")
	}
	// F1 core: request/response describe the JS exchange, not the page.
	if f.Evidence.Request == nil || f.Evidence.Request.URL != jsURL {
		t.Errorf("Request must point at the JS exchange %q, got %#v", jsURL, f.Evidence.Request)
	}
	if f.Evidence.Response == nil || f.Evidence.Response.Status != 200 {
		t.Errorf("Response must describe the JS exchange, got %#v", f.Evidence.Response)
	}
}

// A body match records a body offset that actually indexes the response body.
func TestEvidence_BodyMatch_OffsetIndexesBody(t *testing.T) {
	sig := commentSig(t)
	body := `<!-- token=` + realJWT + ` -->`
	ctx := ctxWith("https", 200, body, "https://h/")

	fs, _, err := sig.Judge(ctx)
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if len(fs) != 1 {
		t.Fatalf("want 1 finding, got %d", len(fs))
	}
	loc := fs[0].Evidence.Items[0].Location
	if loc.Part != domain.PartBody {
		t.Fatalf("want body part, got %q", loc.Part)
	}
	if loc.Offset < 0 || loc.Offset >= len(body) {
		t.Fatalf("offset %d out of body range [0,%d)", loc.Offset, len(body))
	}
	if loc.SourceURL != "" {
		t.Errorf("a page-body match must not set SourceURL, got %q", loc.SourceURL)
	}
}
