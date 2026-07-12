package platform

import "errors"

// ErrBadRequest wraps a caller-input error (e.g. ingest validation failure) so
// the ScanAPI can map it to HTTP 400 without inspecting error strings. The
// orchestrator wraps ingest/validation failures with it; everything else is an
// internal (500) error.
var ErrBadRequest = errors.New("platform: bad request")

// ErrNotFound is returned when a scan id is unknown (→ HTTP 404).
var ErrNotFound = errors.New("platform: scan not found")
