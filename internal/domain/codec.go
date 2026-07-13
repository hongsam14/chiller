package domain

import (
	"encoding/json"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
)

// BSON and JSON value codecs for the kernel's int-iota enums (G2). Every one of
// these is persisted — Severity in the findings collection, ItemStatus in a
// report's coverage, ScopeKind in the work ledger and the queue payload — so all
// three are written as their names, never as ordinals. Reordering or inserting a
// constant then cannot silently reinterpret existing documents, and a missing
// field becomes a decode error rather than the zero value (which for ItemStatus
// would read as "checked").
//
// The marshalers REFUSE to write an invalid value rather than emitting the
// "unknown" that String() renders for one. That asymmetry matters: "unknown" does
// not parse back, so persisting it would poison the document — the next read of
// that scan fails on the bad field and takes every other finding in the cursor
// down with it. Failing the write instead surfaces the bug at the moment it is
// introduced and leaves the previously stored data intact.
//
// The mongo-driver dependency is confined to this file; the enum logic itself
// (String/Parse) stays driver-free.

func (s Severity) MarshalBSONValue() (bsontype.Type, []byte, error) {
	if !s.Valid() {
		return 0, nil, fmt.Errorf("domain: refusing to persist invalid Severity %d", int(s))
	}
	return bson.MarshalValue(s.String())
}

func (s *Severity) UnmarshalBSONValue(t bsontype.Type, data []byte) error {
	var name string
	if err := (bson.RawValue{Type: t, Value: data}).Unmarshal(&name); err != nil {
		return err
	}
	v, ok := ParseSeverity(name)
	if !ok {
		return fmt.Errorf("domain: invalid Severity %q", name)
	}
	*s = v
	return nil
}

func (s Severity) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("domain: refusing to encode invalid Severity %d", int(s))
	}
	return json.Marshal(s.String())
}

func (s *Severity) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return err
	}
	v, ok := ParseSeverity(name)
	if !ok {
		return fmt.Errorf("domain: invalid Severity %q", name)
	}
	*s = v
	return nil
}

func (s ItemStatus) MarshalBSONValue() (bsontype.Type, []byte, error) {
	if !s.valid() {
		return 0, nil, fmt.Errorf("domain: refusing to persist invalid ItemStatus %d", int(s))
	}
	return bson.MarshalValue(s.String())
}

func (s *ItemStatus) UnmarshalBSONValue(t bsontype.Type, data []byte) error {
	var name string
	if err := (bson.RawValue{Type: t, Value: data}).Unmarshal(&name); err != nil {
		return err
	}
	v, ok := ParseItemStatus(name)
	if !ok {
		return fmt.Errorf("domain: invalid ItemStatus %q", name)
	}
	*s = v
	return nil
}

func (s ItemStatus) MarshalJSON() ([]byte, error) {
	if !s.valid() {
		return nil, fmt.Errorf("domain: refusing to encode invalid ItemStatus %d", int(s))
	}
	return json.Marshal(s.String())
}

func (s *ItemStatus) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return err
	}
	v, ok := ParseItemStatus(name)
	if !ok {
		return fmt.Errorf("domain: invalid ItemStatus %q", name)
	}
	*s = v
	return nil
}

func (s ScopeKind) MarshalBSONValue() (bsontype.Type, []byte, error) {
	if !s.Valid() {
		return 0, nil, fmt.Errorf("domain: refusing to persist invalid ScopeKind %d", int(s))
	}
	return bson.MarshalValue(s.String())
}

func (s *ScopeKind) UnmarshalBSONValue(t bsontype.Type, data []byte) error {
	var name string
	if err := (bson.RawValue{Type: t, Value: data}).Unmarshal(&name); err != nil {
		return err
	}
	v, ok := ParseScopeKind(name)
	if !ok {
		return fmt.Errorf("domain: invalid ScopeKind %q", name)
	}
	*s = v
	return nil
}

func (s ScopeKind) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("domain: refusing to encode invalid ScopeKind %d", int(s))
	}
	return json.Marshal(s.String())
}

func (s *ScopeKind) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return err
	}
	v, ok := ParseScopeKind(name)
	if !ok {
		return fmt.Errorf("domain: invalid ScopeKind %q", name)
	}
	*s = v
	return nil
}
