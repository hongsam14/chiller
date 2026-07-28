package observation

import "testing"

// testRules loads the real distilled signature config (configs/signatures/) so the
// config-driven signatures (PS-009/018/013) are exercised against the SAME rules
// that ship — not a fixture that could drift from production behaviour.
func testRules(t *testing.T) Rules {
	t.Helper()
	r, err := LoadRules("../../../../configs/signatures")
	if err != nil {
		t.Fatalf("load signature rules: %v", err)
	}
	return r
}

func commentSig(t *testing.T) sensitiveCommentSig {
	r := testRules(t)
	return sensitiveCommentSig{secrets: r.Secrets, libPaths: r.LibraryPaths, internal: r.Internal}
}
func sessionSig(t *testing.T) sessionInURLSig { return sessionInURLSig{params: testRules(t).URLParams} }
func adminSig(t *testing.T) adminPageSig      { return adminPageSig{apiDocs: testRules(t).APIDocs} }
