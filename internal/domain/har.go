package domain

// HAREntry is the cached-response subset a URL-scoped checker consumes — the
// fields chiller's observation signatures actually need, not full HAR fidelity
// (Q5, NFR-16). Body is bounded and transient: it lives only in the
// ResponseCache and is wiped post-scan; it is never persisted as a finding (FR-12).
type HAREntry struct {
	URL       string
	Scheme    string // "http" | "https"  (→ PS-003 plaintext login form)
	Status    int
	Headers   []Header  // → humble items (HSTS, clickjacking, ...)
	Cookies   []Cookie  // → PS-015/016/017 (Secure/HttpOnly/SameSite)
	Body      []byte    // BOUNDED (max-body-size, NFR-16); HTML or JS
	FetchedAt Timestamp // stamped by the fetcher; opaque to domain
}

// Header is one response header.
type Header struct {
	Name  string
	Value string
}

// Cookie carries the cookie name/value plus the hygiene attributes the
// cookie-flag signatures inspect. Secure and HttpOnly are presence flags
// (PS-015/016); SameSite is a valued attribute, empty when unset (PS-017).
// Attrs holds any other raw attributes verbatim.
type Cookie struct {
	Name     string
	Value    string
	Secure   bool              // PS-015
	HttpOnly bool              // PS-016
	SameSite string            // PS-017; "" when the attribute is absent
	Attrs    map[string]string // any other attributes, verbatim
}
