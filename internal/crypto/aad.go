package crypto

import "strings"

// The AAD builders live here so that a sealed value's binding context is defined
// in exactly one place per sink. Getting these wrong is silent: a mismatch only
// shows up as a decryption failure at read time, and an over-broad AAD (say,
// scan id alone) would let a sealed value be moved between findings within the
// same scan without detection.
//
// The domain type for each part is a string alias, so crypto takes plain strings
// and stays free of any dependency on the kernel.

// aadSep cannot appear in a scan id, work-item id, hex finding key, or URL, so
// concatenated parts cannot be made to collide by choosing adversarial values.
const aadSep = "\x1f"

// EvidenceAAD binds a finding's evidence in the findings collection to the scan
// and the finding it belongs to.
func EvidenceAAD(scanID, findingKey string) []byte {
	return []byte(strings.Join([]string{"findings", scanID, findingKey}, aadSep))
}

// LedgerEvidenceAAD binds a finding's evidence as carried in the work ledger —
// the transport from worker to manager — to the work item that produced it and
// the finding it belongs to. It does not name the scan separately because it does
// not need to: a work item id is "<scanID>#<n>", so binding to the item already
// binds to the scan.
func LedgerEvidenceAAD(itemID, findingKey string) []byte {
	return []byte(strings.Join([]string{"ledger", itemID, findingKey}, aadSep))
}

// CacheAAD binds a cached response to the scan and the URL it was fetched from,
// so a cache entry cannot be passed off as a different URL's response — which
// would turn cache poisoning into a false negative in the observation signatures.
func CacheAAD(scanID, url string) []byte {
	return []byte(strings.Join([]string{"cache", scanID, url}, aadSep))
}
