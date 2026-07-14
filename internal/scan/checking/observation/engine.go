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

// Default returns the 10 observation signatures (r5). Anchors are tight; the
// authoritative CWE↔PS-ID rows are pinned at Functional Design (C-5) — several
// anchors ship real, the harder ones (URL-context-dependent) as safe stubs that
// still own their CWE so coverage stays complete.
func Default() []Signature {
	return []Signature{
		plaintextLoginSig{},   // PS-003
		robotsSensitiveSig{},  // PS-008 (stub anchor)
		sensitiveCommentSig{}, // PS-009
		gitExposureSig{},      // PS-010
		envExposureSig{},      // PS-011
		backupFileSig{},       // PS-012 (stub anchor)
		adminPageSig{},        // PS-013 (stub anchor)
		sessionInURLSig{},     // PS-018
		crossdomainSig{},      // PS-022
		captchaAbsenceSig{},   // PS-023 (stub anchor)
	}
}
