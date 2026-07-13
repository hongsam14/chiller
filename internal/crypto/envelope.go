// Package crypto is the application-level envelope encryption used for the two
// kinds of sensitive data chiller persists: finding evidence (response fragments)
// and cached responses (whole bodies, headers and cookies). Both are sealed
// before they leave the process, so a backend — Mongo, Valkey, their memory
// dumps, RDB files and backups — only ever holds opaque bytes (SECURITY-01).
//
// It deliberately uses only the standard library (crypto/aes + crypto/cipher):
// the alternative, Mongo's client-side field-level encryption, would add a key
// vault plus the mongocryptd/crypt_shared binaries to the image, and buys us
// nothing here because none of the sealed fields are ever queried — they are
// only ever fetched by scan id.
//
// AES-256-GCM is an AEAD, so Open also authenticates: a tampered ciphertext fails
// to decrypt rather than decrypting to garbage (SECURITY-13). Callers bind each
// envelope to its context via the aad argument (e.g. scan id + finding key), which
// makes a sealed value non-transplantable — an attacker with write access to the
// store cannot move one finding's evidence onto another finding, or pass off a
// cached response as a different URL's, because the AAD would no longer match.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

// envelopeVersion is the current envelope layout. It travels with every sealed
// value so a future format change has an explicit branch point.
const envelopeVersion = 1

// Envelope is one sealed value as persisted. Nonce is fresh per Seal; KeyID names
// the key that sealed it, so keys can be rotated without rewriting old data.
type Envelope struct {
	V     int    `bson:"v" json:"v"`
	KeyID string `bson:"keyId" json:"keyId"`
	Nonce []byte `bson:"nonce" json:"nonce"`
	CT    []byte `bson:"ct" json:"ct"`
}

// Empty reports whether e holds no sealed value (the zero Envelope). Storing a
// finding with no evidence is legitimate, and must not be confused with a
// decryption failure.
func (e Envelope) Empty() bool { return len(e.CT) == 0 && len(e.Nonce) == 0 }

// Keyring resolves key ids to keys. Current names the key new values are sealed
// with; Get resolves any key an existing envelope might reference, so a rotated
// key stays readable.
type Keyring interface {
	Current() (id string, key []byte)
	Get(id string) (key []byte, ok bool)
}

type keyring struct {
	current string
	keys    map[string][]byte
}

func (k *keyring) Current() (string, []byte) { return k.current, k.keys[k.current] }

func (k *keyring) Get(id string) ([]byte, bool) {
	key, ok := k.keys[id]
	return key, ok
}

// NewKeyring parses the key material a process was given.
//
//	single key:   "<base64>"                        → id "k1"
//	rotating:     "k2:<base64>,k1:<base64>"         → every key MUST carry its id
//
// The first entry is the current key (what new values are sealed with); the rest
// stay available to open envelopes written under them.
//
// Once there is more than one key, an id is REQUIRED on each. Deriving ids from
// position instead looks convenient and is a trap: the whole point of an id is that
// it still names the same key bytes after the list changes, and a positional scheme
// silently renames every key each time one is prepended — so envelopes written as
// "k1" would be opened with what is now the k1 key, i.e. the wrong one, and fail to
// decrypt. Making the operator write the ids down is the only way the mapping
// survives a rotation.
//
// A malformed or wrong-length key is an error, never a warning: a process that
// silently came up without working encryption would fail every read, and we would
// rather it not start (SECURITY-15).
func NewKeyring(spec string) (Keyring, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("crypto: no key material (set CHILLER_EVIDENCE_KEY)")
	}
	entries := make([]string, 0, 2)
	for _, e := range strings.Split(spec, ",") {
		if e = strings.TrimSpace(e); e != "" {
			entries = append(entries, e)
		}
	}
	if len(entries) == 0 {
		return nil, errors.New("crypto: no usable key in key material")
	}

	kr := &keyring{keys: map[string][]byte{}}
	for i, entry := range entries {
		id, encoded := defaultKeyID, entry
		if idx := strings.Index(entry, ":"); idx >= 0 {
			id, encoded = strings.TrimSpace(entry[:idx]), entry[idx+1:]
		} else if len(entries) > 1 {
			return nil, fmt.Errorf(
				"crypto: key %d of %d has no id — with more than one key every entry must be "+
					`"<id>:<base64>" (e.g. "k2:<new>,k1:<old>"), because an envelope records the id `+
					"it was sealed under", i+1, len(entries))
		}
		if id == "" {
			return nil, fmt.Errorf("crypto: key %d has an empty id", i+1)
		}
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			return nil, fmt.Errorf("crypto: key %q is not valid base64: %w", id, err)
		}
		if len(key) != KeySize {
			return nil, fmt.Errorf("crypto: key %q is %d bytes, want %d (AES-256)", id, len(key), KeySize)
		}
		if _, dup := kr.keys[id]; dup {
			return nil, fmt.Errorf("crypto: duplicate key id %q", id)
		}
		kr.keys[id] = key
		if i == 0 {
			kr.current = id
		}
	}
	return kr, nil
}

// defaultKeyID is what a lone, un-prefixed key is called. It is also the id the
// very first envelopes are sealed under, which is why a rotation must keep naming
// that key "k1".
const defaultKeyID = "k1"

// Seal encrypts plaintext with the keyring's current key, binding it to aad.
// The same plaintext sealed twice yields different ciphertexts (fresh nonce).
func Seal(kr Keyring, plaintext, aad []byte) (Envelope, error) {
	if kr == nil {
		return Envelope{}, errors.New("crypto: nil keyring")
	}
	id, key := kr.Current()
	gcm, err := newGCM(key)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, fmt.Errorf("crypto: nonce: %w", err)
	}
	return Envelope{
		V:     envelopeVersion,
		KeyID: id,
		Nonce: nonce,
		CT:    gcm.Seal(nil, nonce, plaintext, aad),
	}, nil
}

// Open decrypts and authenticates e. It fails if the ciphertext was tampered
// with, if aad does not match the value the envelope was sealed with, or if the
// key that sealed it is not in the keyring.
func Open(kr Keyring, e Envelope, aad []byte) ([]byte, error) {
	if kr == nil {
		return nil, errors.New("crypto: nil keyring")
	}
	if e.V != envelopeVersion {
		return nil, fmt.Errorf("crypto: unsupported envelope version %d", e.V)
	}
	key, ok := kr.Get(e.KeyID)
	if !ok {
		return nil, fmt.Errorf("crypto: unknown key id %q (rotated away?)", e.KeyID)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(e.Nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("crypto: nonce is %d bytes, want %d", len(e.Nonce), gcm.NonceSize())
	}
	pt, err := gcm.Open(nil, e.Nonce, e.CT, aad)
	if err != nil {
		// Includes both tampering and an AAD mismatch — the caller cannot tell
		// them apart, and neither is recoverable.
		return nil, fmt.Errorf("crypto: open (tampered, wrong key, or wrong context): %w", err)
	}
	return pt, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: gcm: %w", err)
	}
	return gcm, nil
}
