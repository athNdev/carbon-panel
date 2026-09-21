package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
)

// leeway is the clock-skew tolerance applied to exp/nbf enforcement.
const leeway = 30 * time.Second

// defaultCacheTTL applies when ClerkConfig.CacheTTL is 0.
const defaultCacheTTL = 15 * time.Minute

// ClerkConfig configures Clerk session JWT verification.
type ClerkConfig struct {
	// Issuer is the Clerk instance issuer, e.g.
	// https://your-app.clerk.accounts.dev. Required; iss must equal it exactly.
	Issuer string
	// JWKSURL overrides the JWKS location. When empty it is derived from
	// Issuer as Issuer + "/.well-known/jwks.json".
	JWKSURL string
	// Audience optionally requires the token audience to contain this value.
	Audience string
	// AuthorizedParties lists allowed azp values. Empty means "do not check"
	// (dev convenience); a warning is logged once at construction.
	AuthorizedParties []string
	// CacheTTL bounds how long a fetched JWKS is used before it is refreshed;
	// unknown kids always trigger an immediate single-flight refetch. 0 = 15m.
	CacheTTL time.Duration
	// HTTPClient optionally carries the client used for JWKS fetches.
	HTTPClient *http.Client
	// Clock optionally overrides time.Now (tests).
	Clock func() time.Time
}

// Claims is the verified subset of a Clerk session token the control plane needs.
type Claims struct {
	Subject   string
	OrgID     string // Clerk org_id claim
	OrgRole   string // Clerk org_role claim, e.g. "org:admin"
	OrgSlug   string
	Email     string
	SessionID string
	Issuer    string
	ExpiresAt time.Time
	IssuedAt  time.Time
}

// Verifier turns a raw bearer token into verified claims.
type Verifier interface {
	Verify(ctx context.Context, rawToken string) (Claims, error)
}

// ClerkVerifier verifies Clerk session JWTs against Clerk's JWKS.
//
// The JWKS key set is long-lived and cached: it is never refetched once per
// request. Unknown kids trigger a single-flight refetch (the strategy
// recommended by OpenID Connect Core §7 for key rotation), and the whole set
// is additionally refreshed once it is older than CacheTTL.
type ClerkVerifier struct {
	issuer string
	jwks   string
	aud    string
	azp    map[string]struct{}
	ttl    time.Duration
	now    func() time.Time

	baseCtx context.Context

	mu    sync.Mutex
	set   *oidc.RemoteKeySet
	setAt time.Time
}

// NewClerkVerifier builds a ClerkVerifier. It logs a warning once when
// AuthorizedParties is empty but still verifies everything else.
func NewClerkVerifier(cfg ClerkConfig) (*ClerkVerifier, error) {
	if strings.TrimSpace(cfg.Issuer) == "" {
		return nil, fmt.Errorf("auth: clerk issuer is required: %w", ErrTokenIssuer)
	}
	jwks := strings.TrimSpace(cfg.JWKSURL)
	if jwks == "" {
		jwks = strings.TrimSuffix(strings.TrimSpace(cfg.Issuer), "/") + "/.well-known/jwks.json"
	}
	ttl := cfg.CacheTTL
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	now := cfg.Clock
	if now == nil {
		now = time.Now
	}
	azp := make(map[string]struct{}, len(cfg.AuthorizedParties))
	for _, a := range cfg.AuthorizedParties {
		azp[a] = struct{}{}
	}
	if len(azp) == 0 {
		log.Printf("auth: clerk AuthorizedParties empty, azp check disabled (dev convenience)")
	}
	base := context.Background()
	if cfg.HTTPClient != nil {
		base = oidc.ClientContext(base, cfg.HTTPClient)
	}
	v := &ClerkVerifier{
		issuer:  strings.TrimSpace(cfg.Issuer),
		jwks:    jwks,
		aud:     cfg.Audience,
		azp:     azp,
		ttl:     ttl,
		now:     now,
		baseCtx: base,
	}
	return v, nil
}

// currentSet returns the cached key set, refreshing it once it is older than
// the TTL. The mutex makes concurrent refreshes single-flight.
func (v *ClerkVerifier) currentSet() *oidc.RemoteKeySet {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.set == nil || v.now().Sub(v.setAt) >= v.ttl {
		v.set = oidc.NewRemoteKeySet(v.baseCtx, v.jwks)
		v.setAt = v.now()
	}
	return v.set
}

// clerkPayload mirrors the token claims we enforce or expose.
type clerkPayload struct {
	Subject   string           `json:"sub"`
	Issuer    string           `json:"iss"`
	Audience  jwt.ClaimStrings `json:"aud"`
	Azp       string           `json:"azp"`
	ExpiresAt *jwt.NumericDate `json:"exp"`
	NotBefore *jwt.NumericDate `json:"nbf"`
	IssuedAt  *jwt.NumericDate `json:"iat"`
	OrgID     string           `json:"org_id"`
	OrgRole   string           `json:"org_role"`
	OrgSlug   string           `json:"org_slug"`
	Email     string           `json:"email"`
	SessionID string           `json:"sid"`
}

// Verify checks signature, algorithm, issuer, lifetime, subject and audience,
// in that order. Every failure denies with a typed error.
func (v *ClerkVerifier) Verify(ctx context.Context, rawToken string) (Claims, error) {
	if strings.TrimSpace(rawToken) == "" {
		return Claims{}, ErrTokenMalformed
	}

	// Pin the algorithm before trusting any key. ParseUnverified decodes
	// structure only; the signature itself is verified below exclusively
	// against keys from the JWKS key set.
	parsed, _, err := jwt.NewParser(jwt.WithValidMethods([]string{"RS256"})).ParseUnverified(rawToken, jwt.MapClaims{})
	if err != nil {
		return Claims{}, ErrTokenMalformed
	}
	if parsed.Method == nil || parsed.Method.Alg() != "RS256" {
		return Claims{}, ErrTokenSignature
	}

	// Signature verified against Clerk's JWKS. The set is cached; an unknown
	// kid triggers one single-flight refetch inside RemoteKeySet.
	payload, err := v.currentSet().VerifySignature(ctx, rawToken)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "malformed"):
			return Claims{}, ErrTokenMalformed
		case strings.Contains(msg, "fetching keys") || strings.Contains(msg, "get keys"):
			return Claims{}, fmt.Errorf("%w: %v", ErrJWKSUnavailable, err)
		default:
			return Claims{}, fmt.Errorf("%w", ErrTokenSignature)
		}
	}

	var p clerkPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return Claims{}, ErrTokenMalformed
	}

	if p.Issuer != v.issuer {
		return Claims{}, ErrTokenIssuer
	}
	if p.Subject == "" {
		return Claims{}, ErrTokenMalformed
	}
	now := v.now()
	if p.ExpiresAt == nil || now.After(p.ExpiresAt.Time.Add(leeway)) {
		return Claims{}, ErrTokenExpired
	}
	if p.NotBefore != nil && now.Add(leeway).Before(p.NotBefore.Time) {
		return Claims{}, ErrTokenExpired
	}
	if len(v.azp) > 0 {
		if _, ok := v.azp[p.Azp]; !ok {
			return Claims{}, ErrTokenAudience
		}
	} else if v.aud != "" && !audienceAllows(p.Audience, p.Azp, v.aud) {
		return Claims{}, ErrTokenAudience
	}

	out := Claims{
		Subject:   p.Subject,
		OrgID:     p.OrgID,
		OrgRole:   p.OrgRole,
		OrgSlug:   p.OrgSlug,
		Email:     p.Email,
		SessionID: p.SessionID,
		Issuer:    p.Issuer,
		IssuedAt:  time.Time{},
	}
	if p.IssuedAt != nil {
		out.IssuedAt = p.IssuedAt.Time
	}
	out.ExpiresAt = p.ExpiresAt.Time
	return out, nil
}

// audienceAllows reports whether the configured audience is satisfied by the
// token's aud claim or, for Clerk session tokens that carry it, the azp claim.
func audienceAllows(aud jwt.ClaimStrings, azp, want string) bool {
	if azp == want {
		return true
	}
	for _, a := range aud {
		if a == want {
			return true
		}
	}
	return false
}

// SessionPrincipal maps verified claims onto a request principal. The Clerk
// org role is an input, never the authorization decision: it is normalized to
// the control plane vocabulary ("org:admin" -> "admin", "org:member" ->
// "operator", "org:billing" -> "billing", anything else -> "viewer") for the
// policy engine to consume.
func SessionPrincipal(c Claims) principal.Principal {
	return principal.Principal{
		Kind:   principal.KindSession,
		UserID: c.Subject,
		OrgID:  c.OrgID,
		Role:   normalizeRole(c.OrgRole),
		Email:  c.Email,
	}
}

// normalizeRole folds a Clerk org role into the control plane vocabulary.
// Unknown and empty roles fail closed to "viewer".
func normalizeRole(role string) string {
	switch strings.TrimPrefix(strings.TrimSpace(role), "org:") {
	case "admin":
		return "admin"
	case "member":
		return "operator"
	case "billing":
		return "billing"
	default:
		return "viewer"
	}
}
