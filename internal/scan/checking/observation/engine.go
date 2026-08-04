// Package observation is the in-process native engine (US-006, SR-6). Each
// signature owns ONE CWE (질문1=A) and judges a fetched response with tight
// anchors; each is exposed as its own domain.Checker (ID "observation:CWE-N")
// so a signature failure isolates to its CWE (R7) and the set yields the
// CWEOwnership findings' coverage needs.
package observation

import "github.com/hongsam14/chiller/internal/domain"

// Signature judges a single CWE over a ScanContext.
type Signature interface {
	CWE() domain.CWE
	// Judge returns findings (issue present), notApplicable (structural N/A, FQ5),
	// or an error (this CWE tool-failed). It must not panic (total, SR-6).
	Judge(domain.ScanContext) (findings []domain.Finding, notApplicable bool, err error)
}

// sigChecker adapts a Signature to domain.Checker.
type sigChecker struct{ sig Signature }

func (c sigChecker) ID() domain.CheckerID {
	return domain.CheckerID("observation:" + string(c.sig.CWE()))
}
func (c sigChecker) Scope() domain.ScopeKind { return domain.URLScoped }
func (c sigChecker) OwnedCWEs() []domain.CWE { return []domain.CWE{c.sig.CWE()} } // registry → CWEOwnership

func (c sigChecker) Check(ctx domain.ScanContext) (domain.CheckResult, error) {
	findings, na, err := c.sig.Judge(ctx)
	if err != nil {
		return domain.CheckResult{}, err // all-or-nothing per checker (R7): this CWE tool-failed
	}
	res := domain.CheckResult{Findings: findings}
	if na {
		res.NotApplicable = []domain.CWE{c.sig.CWE()}
	}
	return res, nil
}

// AsChecker exposes a Signature as a domain.Checker.
func AsChecker(s Signature) domain.Checker { return sigChecker{sig: s} }

// Checkers adapts a set of signatures to domain.Checkers.
func Checkers(sigs []Signature) []domain.Checker {
	out := make([]domain.Checker, len(sigs))
	for i, s := range sigs {
		out[i] = AsChecker(s)
	}
	return out
}

// New returns the 13 observation signatures with the config-driven ones (PS-009
// secrets/internal, PS-018 URL-sensitive, PS-013 api-docs) wired to the distilled
// rules (r16). Every signature OWNS its CWE regardless of rule count — so an empty
// Rules still yields a complete owner set (the 26-partition/boot gate is
// rule-independent); the rules only decide WHAT each detects. Load rules via
// LoadRules and pass them here.
// Default returns the signature set with EMPTY rules — every signature present and
// owning its CWE, but the config-driven detections (PS-009/018/013) inert. It is
// for ownership-only contexts (the boot gate / registry, which need the CWE→PS-ID
// partition, not detection). Runtime detection loads real rules via LoadRules + New.
func Default() []Signature { return New(Rules{}) }

func New(rules Rules) []Signature {
	return []Signature{
		plaintextLoginSig{}, // PS-003
		robotsSensitiveSig{},
		sensitiveCommentSig{secrets: rules.Secrets, libPaths: rules.LibraryPaths, internal: rules.Internal}, // PS-009
		gitExposureSig{}, // PS-010
		envExposureSig{}, // PS-011
		backupFileSig{},  // PS-012
		adminPageSig{apiDocs: rules.APIDocs, exposures: rules.Exposures}, // PS-013 (+product exposures r21)
		sessionInURLSig{params: rules.URLParams},                         // PS-018
		crossdomainSig{},                                                 // PS-022
		captchaAbsenceSig{},                                              // PS-023
		cookieSecureSig{},                                                // PS-015
		cookieHTTPOnlySig{},                                              // PS-016
		cookieSameSiteSig{},                                              // PS-017
		bodyMarkerSig{cwe: cweStackTrace, sub: "stacktrace", rules: rules.StackTrace}, // 26-외 passive signal (PS-006→Nuclei, r22)
		bodyMarkerSig{cwe: cweErrorInfo, sub: "error-info", rules: rules.ErrorInfo},   // 26-외 passive signal (PS-005→Nuclei, r22)
		piiExposureSig{}, // PII exposure (26-외, CWE-359 r21)
	}
}
