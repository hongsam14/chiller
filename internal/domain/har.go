package domain

// HAREntry is the cached-response subset a URL-scoped checker consumes — the
// fields chiller's observation signatures actually need, not full HAR fidelity
// (Q5, NFR-16). Body is bounded and transient: it lives only in the
// ResponseCache and is wiped post-scan; it is never persisted as a finding (FR-12).
type HAREntry struct {
	URL       string    `bson:"url" json:"url"`
	Scheme    string    `bson:"scheme" json:"scheme"` // "http" | "https"  (→ PS-003 plaintext login form)
	Status    int       `bson:"status" json:"status"`
	Headers   []Header  `bson:"headers" json:"headers"`     // → humble items (HSTS, clickjacking, ...)
	Cookies   []Cookie  `bson:"cookies" json:"cookies"`     // → PS-015/016/017 (Secure/HttpOnly/SameSite)
	Body      []byte    `bson:"body" json:"body"`           // BOUNDED (max-body-size, NFR-16); HTML or JS
	FetchedAt Timestamp `bson:"fetchedAt" json:"fetchedAt"` // stamped by the fetcher; opaque to domain
	// Truncated means the body is NOT fully available as decoded text (r17, FR-T3):
	// the decoded output hit the max-body cap, a decode error left it partial, or the
	// Content-Encoding was unknown/unsupported (present but undecoded). A body-based
	// "no finding" on a truncated artifact is not authoritative — it is surfaced in the
	// report, never treated as a complete clean scan. Old docs decode as false.
	Truncated bool `bson:"truncated,omitempty" json:"truncated,omitempty"`
}

// Header is one response header.
type Header struct {
	Name  string `bson:"name" json:"name"`
	Value string `bson:"value" json:"value"`
}

// Cookie carries the cookie name/value plus the hygiene attributes the
// cookie-flag signatures inspect. Secure and HttpOnly are presence flags
// (PS-015/016); SameSite is a valued attribute, empty when unset (PS-017).
// Attrs holds any other raw attributes verbatim.
type Cookie struct {
	Name     string            `bson:"name" json:"name"`
	Value    string            `bson:"value" json:"value"`
	Secure   bool              `bson:"secure" json:"secure"`     // PS-015
	HttpOnly bool              `bson:"httpOnly" json:"httpOnly"` // PS-016
	SameSite string            `bson:"sameSite" json:"sameSite"` // PS-017; "" when the attribute is absent
	Attrs    map[string]string `bson:"attrs" json:"attrs"`       // any other attributes, verbatim
}
