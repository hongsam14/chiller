package taskstore

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/hongsam14/chiller/internal/crypto"
	"github.com/hongsam14/chiller/internal/domain"
)

// sealFindingEvidence hand-seals a raw plaintext under the exact AAD openOutcome
// expects for the given item/finding, and returns the sealedOutcome to open.
func sealFindingEvidence(t *testing.T, kr crypto.Keyring, itemID domain.WorkItemID, f domain.Finding, plaintext []byte) sealedOutcome {
	t.Helper()
	env, err := crypto.Seal(kr, plaintext, crypto.LedgerEvidenceAAD(string(itemID), string(f.Key())))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	return sealedOutcome{
		ID: itemID,
		Findings: []sealedFinding{{
			CWEs: f.CWEs, TargetURL: f.TargetURL, SourceTool: f.SourceTool, Severity: f.Severity, Evidence: env,
		}},
	}
}

// F4: a pre-r20 ledger evidence blob ([]string) must degrade to empty on resume,
// never wedge the whole outcome open.
func TestLedgerEvidenceSeal_OldSchemaDegradesGracefully(t *testing.T) {
	kr := sealKeyring(t)
	m := &Mongo{kr: kr}
	f := domain.Finding{CWEs: []domain.CWE{"CWE-319"}, TargetURL: "https://h/", SourceTool: "humble", Severity: domain.SevMedium}
	oldPlain, _ := json.Marshal([]string{"HSTS missing"})
	got, err := m.openOutcome(sealFindingEvidence(t, kr, "item-1", f, oldPlain))
	if err != nil {
		t.Fatalf("open must not error on an old-schema blob, got %v", err)
	}
	if !got.Findings[0].Evidence.Empty() {
		t.Fatalf("old-schema evidence must degrade to empty, got %#v", got.Findings[0].Evidence)
	}
}

// Authenticated ledger plaintext that is neither the r20 struct nor the legacy
// []string form is genuine corruption — openOutcome must surface it, not silently
// drop evidence and let the resume proceed.
func TestLedgerEvidenceSeal_CorruptPlaintextSurfacesError(t *testing.T) {
	kr := sealKeyring(t)
	m := &Mongo{kr: kr}
	f := domain.Finding{CWEs: []domain.CWE{"CWE-319"}, TargetURL: "https://h/", SourceTool: "humble", Severity: domain.SevMedium}
	if _, err := m.openOutcome(sealFindingEvidence(t, kr, "item-1", f, []byte("42"))); err == nil {
		t.Fatal("open must error on corrupt (non-legacy) authenticated plaintext, got nil")
	}
}

// r20 ledger seal-identity: sealOutcome/openOutcome only use the keyring, so the
// evidence round trip can be exercised white-box without a Mongo container.

func sealKeyring(t *testing.T) crypto.Keyring {
	t.Helper()
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i*13 + 1)
	}
	kr, err := crypto.NewKeyring(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return kr
}

func TestLedgerEvidenceSeal_RoundTripIsIdentity(t *testing.T) {
	m := &Mongo{kr: sealKeyring(t)}
	out := domain.WorkItemOutcome{
		ID: "item-1",
		Findings: []domain.Finding{
			{
				CWEs: []domain.CWE{"CWE-615"}, TargetURL: "https://h/", SourceTool: "observation:CWE-615",
				Severity: domain.SevMedium,
				Evidence: domain.Evidence{
					Items: []domain.EvidenceItem{{
						Snippet:    "apiKey='AbCdEf0123456789'",
						Location:   domain.Location{Part: domain.PartBody, Offset: 12, MatchLen: 16, SourceURL: "https://h/a.js"},
						Rule:       "observation:CWE-615/secret/generic-api-key",
						Hypothesis: "secret in delivered script",
					}},
					Request:  &domain.RequestContext{Method: "GET", URL: "https://h/a.js"},
					Response: &domain.ResponseContext{Status: 200, ContentType: "application/javascript"},
				},
			},
			{
				CWEs: []domain.CWE{"CWE-319"}, TargetURL: "https://h/", SourceTool: "humble",
				Severity: domain.SevMedium,
				Evidence: domain.EvidenceOf("HSTS header missing"), // probe-style, no context
			},
		},
	}
	sealed, err := m.sealOutcome(out)
	if err != nil {
		t.Fatalf("sealOutcome: %v", err)
	}
	got, err := m.openOutcome(sealed)
	if err != nil {
		t.Fatalf("openOutcome: %v", err)
	}
	for i := range out.Findings {
		if !reflect.DeepEqual(got.Findings[i].Evidence, out.Findings[i].Evidence) {
			t.Fatalf("finding %d evidence changed:\n got %#v\nwant %#v",
				i, got.Findings[i].Evidence, out.Findings[i].Evidence)
		}
	}
}
