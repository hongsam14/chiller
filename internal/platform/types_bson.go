package platform

import (
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
)

// BSON value codecs for the FSM enums (Q4=A). They persist ScanState/ItemState
// as their human-readable string form ("running", "inflight", …) rather than as
// iota ordinals, so a document's meaning survives a reordering or insertion of
// the constants (F-4) and the durable CAS filter compares readable values. The
// mongo-driver dependency is confined to this file; the rest of the package's
// enum logic (String/Parse) stays driver-free.

func (s ScanState) MarshalBSONValue() (bsontype.Type, []byte, error) {
	return bson.MarshalValue(s.String())
}

func (s *ScanState) UnmarshalBSONValue(t bsontype.Type, data []byte) error {
	var name string
	if err := (bson.RawValue{Type: t, Value: data}).Unmarshal(&name); err != nil {
		return err
	}
	v, ok := ParseScanState(name)
	if !ok {
		return fmt.Errorf("platform: invalid ScanState %q", name)
	}
	*s = v
	return nil
}

func (s ItemState) MarshalBSONValue() (bsontype.Type, []byte, error) {
	return bson.MarshalValue(s.String())
}

func (s *ItemState) UnmarshalBSONValue(t bsontype.Type, data []byte) error {
	var name string
	if err := (bson.RawValue{Type: t, Value: data}).Unmarshal(&name); err != nil {
		return err
	}
	v, ok := ParseItemState(name)
	if !ok {
		return fmt.Errorf("platform: invalid ItemState %q", name)
	}
	*s = v
	return nil
}
