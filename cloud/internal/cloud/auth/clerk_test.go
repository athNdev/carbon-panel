package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
)

const (
	testIssuer = "https://test.clerk.dev"
	testAzp    = "test-azp"
	testKid1   = "key-1"
	testKid2   = "key-2"
)

// writeJSON encodes the JWKS payload for the test server.
func writeJSON(w http.ResponseWriter, v any) error {
	return json.NewEncoder(w).Encode(v)
}

// jwksServer serves a mutable JWKS and counts fetches.
type jwksServer struct {
	t       *testing.T
	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetches atomic.Int64
	srv     *httptest.Server
}

func newJWKSServer(t *testing.T) *jwksServer {
	t.Helper()
	s := &jwksServer{t: t, keys: map[string]*rsa.PublicKey{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.fetches.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		type jwk struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		}
		out := map[string]any{"keys": []jwk{}}
		list := out["keys"].([]jwk)
		for kid, pub := range s.keys {
			list = append(list, jwk{
				Kty: "RSA", Kid: kid, Use: "sig", Alg: "RS256",
				N: base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			})
		}
		out["keys"] = list
		w.Header().Set("Content-Type", "application/json")
		if err := writeJSON(w, out); err != nil {
			t.Errorf("jwks marshal: %v", err)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *jwksServer) setKey(kid string, pub *rsa.PublicKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[kid] = pub
}

func genKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

func baseClaims() map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":      testIssuer,
		"sub":      "user_123",
		"exp":      now.Add(time.Hour).Unix(),
		"iat":      now.Unix(),
		"nbf":      now.Add(-time.Minute).Unix(),
		"azp":      testAzp,
		"sid":      "sess_abc",
		"org_id":   "org_1",
		"org_role": "org:admin",
		"org_slug": "acme",
		"email":    "ada@example.com",
	}
}

func mint(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims(claims))
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	require.NoError(t, err)
	return s
}

func testVerifier(url string) *ClerkVerifier {
	v, err := NewClerkVerifier(ClerkConfig{
		Issuer:            testIssuer,
		JWKSURL:           url,
		AuthorizedParties: []string{testAzp},
	})
	if err != nil {
		panic(err)
	}
	return v
}

func TestVerifyGoodToken(t *testing.T) {
	t.Parallel()
	key := genKey(t)
	s := newJWKSServer(t)
	s.setKey(testKid1, &key.PublicKey)
	v := testVerifier(s.srv.URL)

	claims, err := v.Verify(context.Background(), mint(t, key, testKid1, baseClaims()))
	require.NoError(t, err)
	require.Equal(t, "user_123", claims.Subject)
	require.Equal(t, "org_1", claims.OrgID)
	require.Equal(t, "org:admin", claims.OrgRole)
	require.Equal(t, "acme", claims.OrgSlug)
	require.Equal(t, "ada@example.com", claims.Email)
	require.Equal(t, "sess_abc", claims.SessionID)
	require.Equal(t, testIssuer, claims.Issuer)
	require.WithinDuration(t, time.Now().Add(time.Hour), claims.ExpiresAt, 2*time.Minute)
}

func TestSessionPrincipalRoles(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"org:admin":   "admin",
		"org:member":  "operator",
		"org:billing": "billing",
		"org:owner":   "viewer",
		"whatever":    "viewer",
		"":            "viewer",
	}
	for in, want := range cases {
		p := SessionPrincipal(Claims{Subject: "u", OrgID: "o", OrgRole: in, Email: "e@x.io"})
		require.Equal(t, want, p.Role, "role %q", in)
		require.Equal(t, principal.KindSession, p.Kind)
		require.Equal(t, "u", p.UserID)
		require.Equal(t, "o", p.OrgID)
		require.Equal(t, "e@x.io", p.Email)
		require.False(t, p.Anonymous())
	}
}

func TestVerifyRejections(t *testing.T) {
	t.Parallel()
	key := genKey(t)
	other := genKey(t)
	s := newJWKSServer(t)
	s.setKey(testKid1, &key.PublicKey)

	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)

	noneTok := func() string {
		tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims(baseClaims()))
		tok.Header["kid"] = testKid1
		str, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
		require.NoError(t, err)
		return str
	}()
	hsTok := func() string {
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(baseClaims()))
		tok.Header["kid"] = testKid1
		str, err := tok.SignedString(der) // public key bytes as HMAC secret
		require.NoError(t, err)
		return str
	}()

	mutate := func(m map[string]any, k string, v any) map[string]any {
		out := map[string]any{}
		for kk, vv := range m {
			out[kk] = vv
		}
		out[k] = v
		return out
	}
	drop := func(m map[string]any, k string) map[string]any {
		out := map[string]any{}
		for kk, vv := range m {
			if kk != k {
				out[kk] = vv
			}
		}
		return out
	}

	now := time.Now()
	cases := []struct {
		name string
		tok  func() string
		want error
	}{
		{"empty", func() string { return "" }, ErrTokenMalformed},
		{"garbage", func() string { return "not.a.token" }, ErrTokenMalformed},
		{"two segments", func() string { return "a.b" }, ErrTokenMalformed},
		{"bad signature", func() string { return mint(t, other, testKid1, baseClaims()) }, ErrTokenSignature},
		{"alg none", func() string { return noneTok }, ErrTokenSignature},
		{"alg HS256", func() string { return hsTok }, ErrTokenSignature},
		{"wrong issuer", func() string { return mint(t, key, testKid1, mutate(baseClaims(), "iss", "https://evil.example")) }, ErrTokenIssuer},
		{"expired", func() string { return mint(t, key, testKid1, mutate(baseClaims(), "exp", now.Add(-time.Hour).Unix())) }, ErrTokenExpired},
		{"not before", func() string {
			return mint(t, key, testKid1, mutate(baseClaims(), "nbf", now.Add(5*time.Minute).Unix()))
		}, ErrTokenExpired},
		{"missing exp", func() string { return mint(t, key, testKid1, drop(baseClaims(), "exp")) }, ErrTokenExpired},
		{"missing sub", func() string { return mint(t, key, testKid1, drop(baseClaims(), "sub")) }, ErrTokenMalformed},
		{"empty sub", func() string { return mint(t, key, testKid1, mutate(baseClaims(), "sub", "")) }, ErrTokenMalformed},
		{"bad azp", func() string { return mint(t, key, testKid1, mutate(baseClaims(), "azp", "other-app")) }, ErrTokenAudience},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := testVerifier(s.srv.URL)
			_, err := v.Verify(context.Background(), tc.tok())
			require.Error(t, err)
			require.True(t, errors.Is(err, tc.want), "want %v got %v", tc.want, err)
		})
	}
}

func TestVerifyLeewayBoundary(t *testing.T) {
	t.Parallel()
	key := genKey(t)
	s := newJWKSServer(t)
	s.setKey(testKid1, &key.PublicKey)
	v := testVerifier(s.srv.URL)

	now := time.Now()
	justExpired := map[string]any{}
	for k, val := range baseClaims() {
		justExpired[k] = val
	}
	justExpired["exp"] = now.Add(-10 * time.Second).Unix() // inside 30s leeway
	_, err := v.Verify(context.Background(), mint(t, key, testKid1, justExpired))
	require.NoError(t, err, "token 10s past exp must pass inside leeway")
}

func TestVerifyAudienceOnly(t *testing.T) {
	t.Parallel()
	key := genKey(t)
	s := newJWKSServer(t)
	s.setKey(testKid1, &key.PublicKey)
	v, err := NewClerkVerifier(ClerkConfig{Issuer: testIssuer, JWKSURL: s.srv.URL, Audience: "my-app"})
	require.NoError(t, err)

	ok := baseClaims()
	ok["azp"] = "my-app"
	delete(ok, "aud")
	_, err = v.Verify(context.Background(), mint(t, key, testKid1, ok))
	require.NoError(t, err)

	bad := baseClaims()
	bad["azp"] = "something-else"
	_, err = v.Verify(context.Background(), mint(t, key, testKid1, bad))
	require.ErrorIs(t, err, ErrTokenAudience)
}

func TestVerifyUnknownKidRotationSingleFlight(t *testing.T) {
	t.Parallel()
	key1 := genKey(t)
	key2 := genKey(t)
	s := newJWKSServer(t)
	s.setKey(testKid1, &key1.PublicKey)
	v := testVerifier(s.srv.URL)

	// Prime the cache with key-1.
	_, err := v.Verify(context.Background(), mint(t, key1, testKid1, baseClaims()))
	require.NoError(t, err)
	require.GreaterOrEqual(t, s.fetches.Load(), int64(1))

	// Rotate: the server now also serves key-2, which the cache has never seen.
	s.setKey(testKid2, &key2.PublicKey)
	before := s.fetches.Load()

	const n = 16
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = v.Verify(context.Background(), mint(t, key2, testKid2, baseClaims()))
		}(i)
	}
	wg.Wait()
	for i := range n {
		require.NoError(t, errs[i], "parallel call %d", i)
	}
	require.Equal(t, int64(1), s.fetches.Load()-before, "unknown-kid refetch must be single-flight")
}

func TestVerifyJWKSUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	v := testVerifier(srv.URL)
	key := genKey(t)
	_, err := v.Verify(context.Background(), mint(t, key, testKid1, baseClaims()))
	require.ErrorIs(t, err, ErrJWKSUnavailable)
}

func TestNewClerkVerifier(t *testing.T) {
	t.Parallel()
	_, err := NewClerkVerifier(ClerkConfig{})
	require.ErrorIs(t, err, ErrTokenIssuer)

	v, err := NewClerkVerifier(ClerkConfig{Issuer: "https://x.clerk.dev/"})
	require.NoError(t, err)
	require.Equal(t, "https://x.clerk.dev/.well-known/jwks.json", v.jwks)
	require.Equal(t, defaultCacheTTL, v.ttl)

	explicit, err := NewClerkVerifier(ClerkConfig{Issuer: testIssuer, JWKSURL: "https://cdn.example/jwks.json"})
	require.NoError(t, err)
	require.Equal(t, "https://cdn.example/jwks.json", explicit.jwks)
}

func TestVerifyTrimsSpace(t *testing.T) {
	t.Parallel()
	key := genKey(t)
	s := newJWKSServer(t)
	s.setKey(testKid1, &key.PublicKey)
	v := testVerifier(s.srv.URL)
	_, err := v.Verify(context.Background(), "   ")
	require.ErrorIs(t, err, ErrTokenMalformed)
}
