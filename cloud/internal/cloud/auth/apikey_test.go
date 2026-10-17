package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyRoundTrip(t *testing.T) {
	t.Parallel()
	pepper := []byte("test-pepper-32-bytes-long........")

	secret, prefix, hash, err := NewAPIKey()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(secret, "cc_"), "secret format")
	require.Equal(t, secret[:12], prefix, "prefix is a prefix")
	require.Len(t, prefix, 12)

	stored := HashAPIKey(pepper, secret)
	require.NotEqual(t, secret, stored, "never store the secret")
	require.Len(t, stored, 64, "hex HMAC-SHA256")

	require.True(t, VerifyAPIKey(pepper, secret, stored))
	// The convenience hash from NewAPIKey verifies under the empty pepper.
	require.True(t, VerifyAPIKey(nil, secret, hash))
	require.Equal(t, hash, HashAPIKey(nil, secret), "deterministic")
}

func TestAPIKeyRejections(t *testing.T) {
	t.Parallel()
	pepper := []byte("pepper")
	secret, _, _, err := NewAPIKey()
	require.NoError(t, err)
	stored := HashAPIKey(pepper, secret)

	require.False(t, VerifyAPIKey(pepper, "cc_wrongsecret", stored), "wrong secret")
	require.False(t, VerifyAPIKey([]byte("other-pepper"), secret, stored), "wrong pepper")
	require.False(t, VerifyAPIKey(pepper, secret, stored[:len(stored)-2]+"ff"), "tampered hash")
	require.False(t, VerifyAPIKey(pepper, secret, "short"), "length mismatch")
	require.False(t, VerifyAPIKey(pepper, "", stored), "empty secret")
	require.False(t, VerifyAPIKey(pepper, secret, ""), "empty hash")
}

func TestAPIKeyUniqueness(t *testing.T) {
	t.Parallel()
	s1, p1, _, err := NewAPIKey()
	require.NoError(t, err)
	s2, p2, _, err := NewAPIKey()
	require.NoError(t, err)
	require.NotEqual(t, s1, s2, "secrets must differ")
	require.NotEqual(t, p1, p2, "prefixes must differ")
	require.Len(t, s1, 3+43, "cc_ + base64url(32 bytes)")
}
