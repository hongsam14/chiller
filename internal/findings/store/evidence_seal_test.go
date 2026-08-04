package store

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/hongsam14/chiller/internal/crypto"
	"github.com/hongsam14/chiller/internal/domain"
)

// r20 seal-identity + graceful-degrade tests for the structured evidence schema.
// sealEvidence/openEvidence only use the keyring (no Mongo I/O), so they can run
// white-box without a container.

func sealKeyring(t *testing.T) crypto.Keyring {
	t.Helper()
	key := make([]byte, crypto.KeySize)
	for i := range key {
		key[i] = byte(i * 7)
	}
	kr, err := crypto.NewKeyring(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return kr
}

// evidenceSamples covers every shape: full structured, context-only (no items),
// probe-style (item, no context), and empty.
func evidenceSamples() map[string]domain.Evidence {
	return map[string]domain.Evidence{
		"full structured": {
			Items: []domain.EvidenceItem{{
				Snippet:    "apiKey = 'AbCdEf0123456789'",
				Location:   domain.Location{Part: domain.PartBody, Offset: 42, MatchLen: 26},
				Rule:       "observation:CWE-615/secret/generic-api-key",
				Hypothesis: "secret hardcoded in delivered script",
			}},
			Request:  &domain.RequestContext{Method: "GET", URL: "https://h/app.js"},
			Response: &domain.ResponseContext{Status: 200, ContentType: "application/javascript", Truncated: false},
		},
		"js-sourced with SourceURL": {
			Items: []domain.EvidenceItem{{
				Snippet:  "token=eyJhbGciOi",
				Location: domain.Location{Part: domain.PartBody, Offset: 7, MatchLen: 9, SourceURL: "https://h/vendor/x.js"},
				Rule:     "observation:CWE-615/secret/jwt",
			}},
			Request:  &domain.RequestContext{Method: "GET", URL: "https://h/vendor/x.js"},
			Response: &domain.ResponseContext{Status: 200, Headers: []domain.Header{{Name: "Server", Value: "nginx"}}},
		},
		"context only (no items)": {
			Request:  &domain.RequestContext{Method: "GET", URL: "https://h/"},
			Response: &domain.ResponseContext{Status: 404},
		},
		"probe-style (item, no context)": {
			Items: []domain.EvidenceItem{{Snippet: "HSTS missing", Rule: "humble", Hypothesis: "cleartext transport"}},
		},
	}
}

func TestEvidenceSeal_RoundTripIsIdentity(t *testing.T) {
	m := &Mongo{kr: sealKeyring(t)}
	const scan, key = domain.ScanID("s1"), domain.FindingKey("k1")
	for name, ev := range evidenceSamples() {
		t.Run(name, func(t *testing.T) {
			env, err := m.sealEvidence(scan, key, ev)
			if err != nil {
				t.Fatalf("seal: %v", err)
			}
			got, err := m.openEvidence(scan, key, env)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if !reflect.DeepEqual(got, ev) {
				t.Fatalf("round-trip changed evidence:\n got %#v\nwant %#v", got, ev)
			}
		})
	}
}

// Empty evidence seals to the zero envelope and opens back to empty (no key needed).
func TestEvidenceSeal_EmptyIsZeroEnvelope(t *testing.T) {
	m := &Mongo{kr: sealKeyring(t)}
	env, err := m.sealEvidence("s", "k", domain.Evidence{})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if !env.Empty() {
		t.Fatalf("empty evidence must seal to the zero envelope")
	}
	got, err := m.openEvidence("s", "k", env)
	if err != nil || !got.Empty() {
		t.Fatalf("open(zero) = (%#v, %v), want empty, nil", got, err)
	}
}

// F4: an old-schema plaintext ([]string) that predates r20 must NOT fail the read —
// openEvidence degrades to empty evidence rather than erroring the whole report.
func TestEvidenceSeal_OldSchemaDegradesGracefully(t *testing.T) {
	kr := sealKeyring(t)
	m := &Mongo{kr: kr}
	const scan, key = domain.ScanID("s1"), domain.FindingKey("k1")
	// Hand-seal an old []string plaintext under the SAME AAD the opener expects.
	oldPlain, _ := json.Marshal([]string{"secret one", "secret two"})
	env, err := crypto.Seal(kr, oldPlain, crypto.EvidenceAAD(string(scan), string(key)))
	if err != nil {
		t.Fatalf("seal old: %v", err)
	}
	got, err := m.openEvidence(scan, key, env)
	if err != nil {
		t.Fatalf("open must not error on an old-schema blob, got %v", err)
	}
	if !got.Empty() {
		t.Fatalf("old-schema evidence must degrade to empty, got %#v", got)
	}
}

// Authenticated plaintext that is neither the r20 struct nor the legacy []string
// form is a genuine corruption of decrypted data — openEvidence must surface it,
// not silently return empty evidence (masking data loss behind a log line).
func TestEvidenceSeal_CorruptPlaintextSurfacesError(t *testing.T) {
	kr := sealKeyring(t)
	m := &Mongo{kr: kr}
	const scan, key = domain.ScanID("s1"), domain.FindingKey("k1")
	// A bare JSON number unmarshals into neither the struct nor []string.
	env, err := crypto.Seal(kr, []byte("42"), crypto.EvidenceAAD(string(scan), string(key)))
	if err != nil {
		t.Fatalf("seal corrupt: %v", err)
	}
	if _, err := m.openEvidence(scan, key, env); err == nil {
		t.Fatal("open must error on corrupt (non-legacy) authenticated plaintext, got nil")
	}
}
