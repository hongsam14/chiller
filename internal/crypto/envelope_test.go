package crypto

import (
	"bytes"
	"encoding/base64"
	"math/rand"
	"testing"
)

func testKeyring(t *testing.T) Keyring {
	t.Helper()
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i)
	}
	kr, err := NewKeyring(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return kr
}

func TestNewKeyring_RejectsBadMaterial(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	for name, spec := range map[string]string{
		"empty":       "",
		"not base64":  "not-base64!!",
		"wrong size":  short,
		"duplicate":   "k1:" + short + ",k1:" + short,
		"blank entry": "   ",
	} {
		if _, err := NewKeyring(spec); err == nil {
			t.Errorf("%s: NewKeyring(%q) should fail closed, got nil error", name, spec)
		}
	}
}

func TestKeyring_RotationKeepsOldKeysReadable(t *testing.T) {
	k1 := make([]byte, KeySize)
	k2 := make([]byte, KeySize)
	for i := range k2 {
		k2[i] = 0xAA
	}
	b64 := base64.StdEncoding.EncodeToString

	// Sealed under the old keyring, where k1 is current.
	old, err := NewKeyring("k1:" + b64(k1))
	if err != nil {
		t.Fatal(err)
	}
	env, err := Seal(old, []byte("secret"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	if env.KeyID != "k1" {
		t.Fatalf("sealed with %q, want k1", env.KeyID)
	}

	// After rotation k2 is current, but k1 must still open existing envelopes.
	rotated, err := NewKeyring("k2:" + b64(k2) + ",k1:" + b64(k1))
	if err != nil {
		t.Fatal(err)
	}
	pt, err := Open(rotated, env, []byte("aad"))
	if err != nil {
		t.Fatalf("rotated keyring cannot open k1 envelope: %v", err)
	}
	if string(pt) != "secret" {
		t.Errorf("got %q, want secret", pt)
	}
	if id, _ := rotated.Current(); id != "k2" {
		t.Errorf("current key is %q, want k2 (first entry)", id)
	}
}

// TestPBT_SealOpenRoundTrip is the round-trip property (PBT-02): whatever we
// seal, we get back byte-for-byte.
func TestPBT_SealOpenRoundTrip(t *testing.T) {
	kr := testKeyring(t)
	rng := rand.New(rand.NewSource(20260713))
	for i := 0; i < 500; i++ {
		pt := randBytes(rng, rng.Intn(4096))
		aad := randBytes(rng, rng.Intn(64))
		env, err := Seal(kr, pt, aad)
		if err != nil {
			t.Fatalf("seed 20260713 iter %d: Seal: %v", i, err)
		}
		got, err := Open(kr, env, aad)
		if err != nil {
			t.Fatalf("seed 20260713 iter %d: Open: %v", i, err)
		}
		if !bytes.Equal(got, pt) {
			t.Fatalf("seed 20260713 iter %d: round-trip mismatch", i)
		}
	}
}

// TestPBT_WrongAADAlwaysFails is the non-transplantability property (PBT-03):
// an envelope sealed in one context must never open in another. This is what
// stops one finding's evidence from being moved onto another finding, or a
// cached response from being passed off as a different URL's.
func TestPBT_WrongAADAlwaysFails(t *testing.T) {
	kr := testKeyring(t)
	rng := rand.New(rand.NewSource(99))
	for i := 0; i < 300; i++ {
		pt := randBytes(rng, 1+rng.Intn(256))
		aad := randBytes(rng, 1+rng.Intn(32))
		other := randBytes(rng, 1+rng.Intn(32))
		if bytes.Equal(aad, other) {
			continue
		}
		env, err := Seal(kr, pt, aad)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Open(kr, env, other); err == nil {
			t.Fatalf("seed 99 iter %d: opened with the wrong AAD — envelope is transplantable", i)
		}
	}
}

// TestPBT_TamperedCiphertextFails is the integrity property (SECURITY-13):
// flipping any bit of the ciphertext or nonce must be detected, not silently
// decrypted to garbage.
func TestPBT_TamperedCiphertextFails(t *testing.T) {
	kr := testKeyring(t)
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 300; i++ {
		pt := randBytes(rng, 1+rng.Intn(256))
		aad := randBytes(rng, 8)
		env, err := Seal(kr, pt, aad)
		if err != nil {
			t.Fatal(err)
		}
		// Flip one bit somewhere in the ciphertext or the nonce.
		target := env.CT
		if rng.Intn(2) == 0 && len(env.Nonce) > 0 {
			target = env.Nonce
		}
		pos := rng.Intn(len(target))
		target[pos] ^= 1 << uint(rng.Intn(8))

		if _, err := Open(kr, env, aad); err == nil {
			t.Fatalf("seed 7 iter %d: tampered envelope opened cleanly", i)
		}
	}
}

// TestSeal_FreshNonceEachTime guards the one mistake that would quietly destroy
// GCM's security: reusing a nonce across two seals under the same key.
func TestSeal_FreshNonceEachTime(t *testing.T) {
	kr := testKeyring(t)
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		env, err := Seal(kr, []byte("same plaintext every time"), []byte("same aad"))
		if err != nil {
			t.Fatal(err)
		}
		n := string(env.Nonce)
		if seen[n] {
			t.Fatalf("nonce reused after %d seals", i)
		}
		seen[n] = true
	}
}

func TestOpen_UnknownKeyID(t *testing.T) {
	kr := testKeyring(t)
	env, err := Seal(kr, []byte("x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	env.KeyID = "nope"
	if _, err := Open(kr, env, nil); err == nil {
		t.Error("Open with an unknown key id should fail")
	}
}

func TestEnvelope_Empty(t *testing.T) {
	if !(Envelope{}).Empty() {
		t.Error("zero Envelope should report Empty")
	}
	kr := testKeyring(t)
	env, err := Seal(kr, []byte(""), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Sealing the empty plaintext still produces a GCM tag, so it is NOT Empty —
	// "no evidence" and "evidence that happens to be empty" stay distinguishable.
	if env.Empty() {
		t.Error("a sealed empty plaintext should not report Empty")
	}
}

func TestAAD_DistinctPerSink(t *testing.T) {
	// The three sinks must never produce the same AAD for the same ids, or a
	// value could be moved between sinks.
	e := EvidenceAAD("scan-1", "key-1")
	l := LedgerEvidenceAAD("scan-1#0", "key-1")
	c := CacheAAD("scan-1", "https://x/y")
	if bytes.Equal(e, l) || bytes.Equal(e, c) || bytes.Equal(l, c) {
		t.Error("AAD builders collide across sinks")
	}
}

func randBytes(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Intn(256))
	}
	return b
}

// TestNewKeyring_MultiKeyRequiresExplicitIDs is the regression for a rotation that
// would have destroyed access to all existing data. Ids used to be derived from
// position, so prepending a new key silently renamed every other one: envelopes
// written under "k1" would then be opened with whatever key had become k1 — the
// wrong one — and fail to decrypt. Once there is more than one key, each must say
// which id it is.
func TestNewKeyring_MultiKeyRequiresExplicitIDs(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString
	k1, k2 := make([]byte, KeySize), make([]byte, KeySize)
	for i := range k2 {
		k2[i] = 0xBB
	}

	// The shape an operator would reach for if ids were optional.
	for _, spec := range []string{
		b64(k2) + "," + b64(k1),         // both bare
		"k2:" + b64(k2) + "," + b64(k1), // new one named, old one bare
	} {
		if _, err := NewKeyring(spec); err == nil {
			t.Errorf("NewKeyring(%.20s…) should refuse a multi-key spec with an unnamed key", spec)
		}
	}

	// The documented rotation must work, and must still open what "k1" sealed.
	original, err := NewKeyring(b64(k1)) // a lone bare key is "k1"
	if err != nil {
		t.Fatal(err)
	}
	env, err := Seal(original, []byte("evidence"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	if env.KeyID != defaultKeyID {
		t.Fatalf("a lone key sealed under %q, want %q", env.KeyID, defaultKeyID)
	}

	rotated, err := NewKeyring("k2:" + b64(k2) + ",k1:" + b64(k1))
	if err != nil {
		t.Fatalf("the documented rotation form was rejected: %v", err)
	}
	pt, err := Open(rotated, env, []byte("aad"))
	if err != nil {
		t.Fatalf("after rotation, data sealed under k1 could not be opened: %v", err)
	}
	if string(pt) != "evidence" {
		t.Errorf("got %q", pt)
	}
	if id, _ := rotated.Current(); id != "k2" {
		t.Errorf("new values should seal under k2, got %q", id)
	}
}
