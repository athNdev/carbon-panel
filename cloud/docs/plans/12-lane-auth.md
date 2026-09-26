# Lane `auth` — Clerk verification, API keys, webhooks

Branch: `cloud/auth`
Write scope: `cloud/internal/cloud/auth/**`

Depends on: `cloud/internal/cloud/principal`, `cloud/internal/cloud/keys`
(both already merged, import them).

Do **not** import `config` or `db`: take explicit structs so this lane stays
parallel to `cfg` and `db`.

## Goal

Turn a bearer credential into a `principal.Principal` without ever calling Clerk
per request, and without ever trusting the client. Read ADR 0002 first.

## Clerk session verification

```go
type ClerkConfig struct {
    Issuer            string        // e.g. https://your-app.clerk.accounts.dev
    JWKSURL           string        // optional; derived from Issuer when empty
    Audience          string        // optional azp/audience check
    AuthorizedParties []string      // allowed azp values; empty means "do not check"
    CacheTTL          time.Duration // JWKS cache lifetime; 0 = 15m
    HTTPClient        *http.Client  // optional
    Clock             func() time.Time // optional, for tests
}

type Claims struct {
    Subject   string
    OrgID     string   // Clerk org_id claim
    OrgRole   string   // Clerk org_role claim, e.g. "org:admin"
    OrgSlug   string
    Email     string
    SessionID string
    Issuer    string
    ExpiresAt time.Time
    IssuedAt  time.Time
}

type Verifier interface {
    Verify(ctx context.Context, rawToken string) (Claims, error)
}

func NewClerkVerifier(cfg ClerkConfig) (*ClerkVerifier, error)
```

Implementation requirements — these are the security requirements, not suggestions:

1. Signature verified against Clerk's JWKS using
   `github.com/coreos/go-oidc/v3/oidc.NewRemoteKeySet` with a cached key set
   refetched on unknown `kid` (single-flight; never once per request).
2. Algorithm pinned to RS256. Reject `alg: none`, HS*, and anything else **before**
   trusting the key — parse with `golang-jwt/jwt/v5` using
   `jwt.WithValidMethods([]string{"RS256"})` plus an explicit `jwt.Keyfunc` that
   only returns keys from the key set.
3. `iss` must equal the configured issuer exactly. `exp` and `nbf` enforced with a
   small leeway (30s). `sub` must be non-empty.
4. When `AuthorizedParties` is non-empty the token's `azp` must be in it; when it
   is empty, log a warning once at construction (dev convenience) but still verify
   everything else.
5. Every failure returns a typed error:
   `ErrTokenMalformed`, `ErrTokenExpired`, `ErrTokenSignature`, `ErrTokenIssuer`,
   `ErrTokenAudience`, `ErrJWKSUnavailable` — callers map these to 401/503.
6. No token, no subject, or a JWKS fetch failure must be distinguishable: a JWKS
   outage is `ErrJWKSUnavailable`, never a silent allow.

```go
// SessionPrincipal maps verified claims onto a request principal.
// The org role string is normalized: "org:admin" -> "admin", "org:member" ->
// "operator", "org:billing" -> "billing", unknown -> "viewer".
func SessionPrincipal(c Claims) principal.Principal
```

## API keys

```go
// NewAPIKey mints a key. `secret` is returned once; only `hash` is stored.
func NewAPIKey() (secret, prefix, hash string, err error)

// HashAPIKey is deterministic so a lookup by hash is possible.
// Use HMAC-SHA256 with a server-side pepper; return hex.
func HashAPIKey(pepper []byte, secret string) string

// VerifyAPIKey compares in constant time.
func VerifyAPIKey(pepper []byte, secret, hash string) bool
```

- Secret format: `cc_` + base64url of 32 random bytes from `crypto/rand`.
- `prefix` is the first 12 characters of the secret (safe to display, enough to
  identify a key in a list).
- Hash is HMAC-SHA256(pepper, secret), hex encoded. Never store the secret.

## Clerk webhooks (Svix scheme)

```go
type WebhookConfig struct {
    Secret    string        // base64, no "whsec_" prefix required
    Tolerance time.Duration // default 5m
    Clock     func() time.Time
}

type WebhookEvent struct {
    Type      string          // e.g. "organization.created"
    Timestamp time.Time
    ID        string
    Payload   json.RawMessage
}

func VerifyWebhook(cfg WebhookConfig, headers http.Header, body []byte) (*WebhookEvent, error)
```

Svix signature scheme (implement directly; do not add a dependency):
- Headers: `svix-id`, `svix-timestamp`, `svix-signature`.
- Signed content: `"{svix-id}.{svix-timestamp}.{body}"`.
- Signature: base64(HMAC-SHA256(base64decode(secret), signedContent)); compare
  against each space-separated `v1,<sig>` in `svix-signature` in constant time.
- Reject when the timestamp is outside the tolerance (replay protection), when any
  header is missing, or when no signature matches.
- Parse a typed payload for at least these event types and expose the fields the
  org sync needs: `organization.created`, `organization.updated`,
  `organization.deleted`, `organizationMembership.created`,
  `organizationMembership.updated`, `organizationMembership.deleted`,
  `user.deleted`. Keep unknown types as raw payload — do not fail.

## Tests

All local, no network:
- Build a test JWKS from a generated 2048-bit RSA key, serve it with `httptest`,
  mint tokens with `golang-jwt/jwt/v5`.
- Accept a good token; reject: bad signature, `alg: none`, HS256 signed with the
  public-key bytes as the HMAC secret, wrong issuer, expired, missing `sub`,
  `azp` not in `AuthorizedParties`, unknown `kid` followed by a JWKS rotation that
  adds the key (must succeed and must have refetched exactly once across N parallel
  calls — assert the single-flight behaviour with an atomic counter).
- `ErrJWKSUnavailable` when the JWKS endpoint 500s.
- API key: mint → hash → verify round trip; wrong secret fails; prefix is a prefix.
- Webhook: valid signature accepted; tampered body, stale timestamp, wrong
  secret, and missing headers all rejected; unknown event type preserved.

## Acceptance

```sh
gofmt -l ./cloud/... # empty
go build ./cloud/... && go vet ./cloud/... && go test ./cloud/...
git diff --stat go.mod go.sum   # must be empty
```

Report per conventions §6.
