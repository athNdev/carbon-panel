package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
)

// apiKeyRandomBytes is the entropy per key; base64url(32 bytes) = 43 chars.
const apiKeyRandomBytes = 32

// NewAPIKey mints a key. secret is returned once and never stored; only the
// hash is stored. The returned hash uses HashAPIKey with an empty pepper as a
// convenience default — deployments with a server-side pepper must store
// HashAPIKey(pepper, secret) instead and verify with VerifyAPIKey.
func NewAPIKey() (secret, prefix, hash string, err error) {
	var b [apiKeyRandomBytes]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", "", err
	}
	secret = "cc_" + base64.RawURLEncoding.EncodeToString(b[:])
	prefix = secret[:12]
	hash = HashAPIKey(nil, secret)
	return secret, prefix, hash, nil
}

// HashAPIKey is deterministic so a lookup by hash is possible. It is
// HMAC-SHA256(pepper, secret), hex encoded. Never store the secret.
func HashAPIKey(pepper []byte, secret string) string {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(secret))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyAPIKey compares in constant time. Empty inputs never verify.
func VerifyAPIKey(pepper []byte, secret, hash string) bool {
	if secret == "" || hash == "" {
		return false
	}
	want := HashAPIKey(pepper, secret)
	if len(want) != len(hash) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(hash)) == 1
}
