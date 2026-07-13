package domain

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// The three enums below are all persisted. These tests pin the two properties
// that make persisting them safe: the stored form is the NAME (not the ordinal),
// and an unrecognized or absent name is an error rather than a silent zero value.

func TestSeverity_BSONRoundTripsByName(t *testing.T) {
	type doc struct {
		Sev Severity `bson:"sev"`
	}
	for _, want := range []Severity{SevInfo, SevLow, SevMedium, SevHigh, SevCritical} {
		raw, err := bson.Marshal(doc{Sev: want})
		if err != nil {
			t.Fatal(err)
		}
		// The stored value must be the string, not the ordinal.
		var probe struct {
			Sev string `bson:"sev"`
		}
		if err := bson.Unmarshal(raw, &probe); err != nil {
			t.Fatalf("%v is not stored as a string: %v", want, err)
		}
		if probe.Sev != want.String() {
			t.Errorf("stored %q, want %q", probe.Sev, want.String())
		}

		var got doc
		if err := bson.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.Sev != want {
			t.Errorf("round-trip: got %v, want %v", got.Sev, want)
		}
	}
}

func TestSeverity_ParseIsCaseInsensitiveAndRejectsUnknown(t *testing.T) {
	for _, in := range []string{"critical", "Critical", "CRITICAL", " critical "} {
		got, ok := ParseSeverity(in)
		if !ok || got != SevCritical {
			t.Errorf("ParseSeverity(%q) = %v,%v; want critical,true", in, got, ok)
		}
	}
	for _, in := range []string{"", "sever", "5", "unknown"} {
		if _, ok := ParseSeverity(in); ok {
			t.Errorf("ParseSeverity(%q) should be rejected", in)
		}
	}
}

// TestItemStatus_MissingFieldIsAnError is the point of the whole exercise. With
// ItemStatus stored as an ordinal, StatusChecked is 0 — so a document that lost
// its status field would decode as "chiller checked this item", turning a storage
// fault into a compliance false negative. Storing the name makes it a decode error.
func TestItemStatus_MissingFieldIsAnError(t *testing.T) {
	type doc struct {
		Status ItemStatus `bson:"status"`
	}
	// A document with no status field at all.
	raw, err := bson.Marshal(bson.M{"other": 1})
	if err != nil {
		t.Fatal(err)
	}
	var got doc
	err = bson.Unmarshal(raw, &got)
	if err == nil && got.Status == StatusChecked {
		t.Fatal("a missing status decoded as StatusChecked — the false-negative this design exists to prevent")
	}

	// An ordinal left over from the old layout must not decode either.
	raw, err = bson.Marshal(bson.M{"status": 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := bson.Unmarshal(raw, &got); err == nil {
		t.Error("an int status should be rejected, not read as StatusChecked")
	}
}

func TestItemStatus_BSONRoundTripsByName(t *testing.T) {
	type doc struct {
		Status ItemStatus `bson:"status"`
	}
	for _, want := range []ItemStatus{StatusChecked, StatusNotApplicable, StatusDelegated, StatusToolFailed} {
		raw, err := bson.Marshal(doc{Status: want})
		if err != nil {
			t.Fatal(err)
		}
		var probe struct {
			Status string `bson:"status"`
		}
		if err := bson.Unmarshal(raw, &probe); err != nil || probe.Status != want.String() {
			t.Fatalf("%v stored as %q (err %v), want %q", want, probe.Status, err, want.String())
		}
		var got doc
		if err := bson.Unmarshal(raw, &got); err != nil || got.Status != want {
			t.Errorf("round-trip %v: got %v (err %v)", want, got.Status, err)
		}
	}
	if _, ok := ParseItemStatus("nonsense"); ok {
		t.Error("ParseItemStatus should reject an unknown name")
	}
}

func TestScopeKind_BSONRoundTripsByName(t *testing.T) {
	type doc struct {
		Scope ScopeKind `bson:"scope"`
	}
	for _, want := range []ScopeKind{URLScoped, HostScoped} {
		raw, err := bson.Marshal(doc{Scope: want})
		if err != nil {
			t.Fatal(err)
		}
		var probe struct {
			Scope string `bson:"scope"`
		}
		if err := bson.Unmarshal(raw, &probe); err != nil || probe.Scope != want.String() {
			t.Fatalf("%v stored as %q (err %v), want %q", want, probe.Scope, err, want.String())
		}
		var got doc
		if err := bson.Unmarshal(raw, &got); err != nil || got.Scope != want {
			t.Errorf("round-trip %v: got %v (err %v)", want, got.Scope, err)
		}
	}
	if _, ok := ParseScopeKind("subdomain"); ok {
		t.Error("ParseScopeKind should reject an unknown name")
	}
}

func TestFindingKey_StableAndOrderIndependent(t *testing.T) {
	a := Finding{CWEs: []CWE{"CWE-614", "CWE-1004"}, TargetURL: "https://x/y", SourceTool: "humble", Severity: SevLow}
	b := Finding{CWEs: []CWE{"CWE-1004", "CWE-614"}, TargetURL: "https://x/y", SourceTool: "humble", Severity: SevHigh}
	if a.Key() != b.Key() {
		t.Error("Key must normalize CWEs — emission order should not change identity")
	}
	if a.Key() == (Finding{CWEs: []CWE{"CWE-614"}, TargetURL: "https://x/y", SourceTool: "humble"}).Key() {
		t.Error("different CWE sets must yield different keys")
	}
	// Encoding-safe: the raw IdentityKey carries a 0x1f separator; the key must not.
	for _, c := range string(a.Key()) {
		if c < '0' || (c > '9' && c < 'a') || c > 'f' {
			t.Fatalf("Key is not hex: %q", a.Key())
		}
	}
}

// TestMarshal_RefusesInvalidEnums. String() renders an invalid value as "unknown",
// which does NOT parse back — so persisting it would poison the document: the next
// read of that scan fails on the bad field and takes every other finding in the
// cursor with it. The write has to fail instead, at the moment the bug is made.
func TestMarshal_RefusesInvalidEnums(t *testing.T) {
	if _, err := bson.Marshal(struct {
		Sev Severity `bson:"sev"`
	}{Sev: SevUnset}); err == nil {
		t.Error("marshaling an unset Severity should fail, not write \"unknown\"")
	}
	if _, err := bson.Marshal(struct {
		Status ItemStatus `bson:"status"`
	}{Status: StatusUnset}); err == nil {
		t.Error("marshaling an unset ItemStatus should fail, not write \"unknown\"")
	}
	if _, err := bson.Marshal(struct {
		Scope ScopeKind `bson:"scope"`
	}{Scope: ScopeUnset}); err == nil {
		t.Error("marshaling an unset ScopeKind should fail, not write \"unknown\"")
	}
	// The valid values still marshal.
	if _, err := bson.Marshal(struct {
		Scope ScopeKind `bson:"scope"`
	}{Scope: HostScoped}); err != nil {
		t.Errorf("a valid ScopeKind should marshal: %v", err)
	}
}

// TestScopeKind_UnsetIsNotAScope: a work item that lost its scope must not read as
// URL-scoped. It used to, because URLScoped was the zero value — and the coverage
// fold would then credit the HOST-scoped checkers (testssl, dirhunt, subzy) with
// having checked their items when nothing had run them.
func TestScopeKind_UnsetIsNotAScope(t *testing.T) {
	if ScopeUnset.Valid() {
		t.Error("ScopeUnset must never be Valid")
	}
	if URLScoped == ScopeUnset || HostScoped == ScopeUnset {
		t.Error("the real scopes must not collide with the zero value")
	}
	if _, ok := ParseScopeKind(""); ok {
		t.Error("an empty scope name must be rejected")
	}
}
