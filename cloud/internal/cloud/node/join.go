package node

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/keys"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
)

// Typed join-token denials. Tampered/unknown secrets share ErrTokenUnknown so
// failures leak nothing about which part was wrong.
var (
	ErrTokenUnknown  = errors.New("node: join token unknown")
	ErrTokenExpired  = errors.New("node: join token expired")
	ErrTokenRevoked  = errors.New("node: join token revoked")
	ErrTokenRedeemed = errors.New("node: join token already redeemed")
	ErrTokenOrg      = errors.New("node: join token org mismatch")
	ErrPepperMissing = errors.New("node: join token pepper not configured")
)

// DefaultJoinTTL applies when IssueRequest carries no TTL or expiry.
const DefaultJoinTTL = time.Hour

// secretPrefix marks presented join secrets.
const secretPrefix = "ccj_"

// JoinTokenService issues and redeems single-use org-bound join tokens.
// The presented secret is shown once; only its HMAC is stored.
type JoinTokenService struct {
	deps Deps
}

// NewJoinTokenService returns a token service over deps.
func NewJoinTokenService(deps Deps) *JoinTokenService {
	return &JoinTokenService{deps: deps}
}

// IssueRequest carries the fields for a new join token.
type IssueRequest struct {
	Name        string
	NodeTypeID  string
	Origin      string
	ProvisionID string
	// TTL sets expiry relative to now. Ignored when ExpiresAt is set.
	// Non-positive TTL selects DefaultJoinTTL.
	TTL time.Duration
	// ExpiresAt overrides TTL. Tests use it to mint expired tokens.
	ExpiresAt *time.Time
}

// Issue creates a token row and returns the one-time secret alongside it.
// The secret format is ccj_<token-id>.<hmac-base64url>.
func (s *JoinTokenService) Issue(ctx context.Context, req IssueRequest) (string, db.JoinToken, error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return "", db.JoinToken{}, err
	}
	pepper, err := s.pepper(ctx)
	if err != nil {
		return "", db.JoinToken{}, err
	}
	if req.Name == "" {
		return "", db.JoinToken{}, errors.New("node: token name required")
	}
	now := s.deps.now()
	exp := req.ExpiresAt
	if exp == nil {
		ttl := req.TTL
		if ttl <= 0 {
			ttl = DefaultJoinTTL
		}
		t := now.Add(ttl)
		exp = &t
	}
	tok := db.JoinToken{
		Name:        req.Name,
		NodeTypeID:  req.NodeTypeID,
		Origin:      req.Origin,
		ProvisionID: req.ProvisionID,
		ExpiresAt:   exp,
	}
	tok.TenantBase = db.TenantBase{ID: newUUID(), OrgID: principal.OrgID(ctx)}
	mac := tokenMAC(pepper, tok.OrgID, tok.ID, *exp)
	tok.SecretHash = b64(mac)
	if err := q.Create(&tok).Error; err != nil {
		return "", db.JoinToken{}, err
	}
	return secretPrefix + tok.ID + "." + b64(mac), tok, nil
}

// List returns the org's tokens, newest first. Hashes only; secrets are
// never recoverable after Issue returns.
func (s *JoinTokenService) List(ctx context.Context) ([]db.JoinToken, error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, err
	}
	var out []db.JoinToken
	if err := q.Order("created_at DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// Revoke marks a token revoked; outstanding secrets stop redeeming.
func (s *JoinTokenService) Revoke(ctx context.Context, id string) (db.JoinToken, error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return db.JoinToken{}, err
	}
	var tok db.JoinToken
	if err := q.Where("id = ?", id).First(&tok).Error; err != nil {
		return db.JoinToken{}, ErrTokenUnknown
	}
	now := s.deps.now()
	tok.RevokedAt = &now
	sq, err := s.deps.Store.Org(ctx)
	if err != nil {
		return db.JoinToken{}, err
	}
	if err := sq.Save(&tok).Error; err != nil {
		return db.JoinToken{}, err
	}
	return tok, nil
}

// Redeem validates a presented secret and claims the token with one atomic
// conditional UPDATE, so exactly one concurrent caller can succeed and the
// rest see ErrTokenRedeemed. The org comes from the token row: when ctx
// carries an org it must match (ErrTokenOrg otherwise). A context without
// an org is the node-join path and is allowed.
func (s *JoinTokenService) Redeem(ctx context.Context, secret string) (db.JoinToken, error) {
	pepper, err := s.pepper(ctx)
	if err != nil {
		return db.JoinToken{}, err
	}
	id, mac, ok := parseSecret(secret)
	if !ok {
		return db.JoinToken{}, ErrTokenUnknown
	}
	raw := s.deps.Store.Unscoped().WithContext(ctx)
	now := s.deps.now()

	var tok db.JoinToken
	if err := raw.Where("id = ?", id).First(&tok).Error; err != nil {
		return db.JoinToken{}, ErrTokenUnknown
	}
	if tok.ExpiresAt == nil {
		return db.JoinToken{}, ErrTokenUnknown
	}
	want := tokenMAC(pepper, tok.OrgID, tok.ID, *tok.ExpiresAt)
	if subtle.ConstantTimeCompare(want, mac) != 1 ||
		subtle.ConstantTimeCompare([]byte(tok.SecretHash), []byte(b64(mac))) != 1 {
		return db.JoinToken{}, ErrTokenUnknown
	}
	if ctxOrg := principal.OrgID(ctx); ctxOrg != "" && ctxOrg != tok.OrgID {
		return db.JoinToken{}, ErrTokenOrg
	}
	if tok.RevokedAt != nil {
		return db.JoinToken{}, ErrTokenRevoked
	}
	if !now.Before(*tok.ExpiresAt) {
		return db.JoinToken{}, ErrTokenExpired
	}
	if tok.UsedAt != nil {
		return db.JoinToken{}, ErrTokenRedeemed
	}

	// Atomic claim: the WHERE fence re-checks everything the read checked,
	// so a concurrent redeemer or revoker loses here, not after.
	claimed, err := s.claim(ctx, tok.ID, now)
	if err != nil {
		return db.JoinToken{}, err
	}
	if !claimed {
		return db.JoinToken{}, s.classifyLoss(ctx, tok.ID)
	}
	var out db.JoinToken
	if err := raw.Where("id = ?", tok.ID).First(&out).Error; err != nil {
		return db.JoinToken{}, ErrTokenUnknown
	}
	return out, nil
}

// claim runs the fenced UPDATE with bounded retries on SQLITE_BUSY so
// concurrent redeemers serialize instead of failing with a lock error.
func (s *JoinTokenService) claim(ctx context.Context, id string, now time.Time) (bool, error) {
	var last error
	for attempt := 0; attempt < 50; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Millisecond)
		}
		res := s.deps.Store.Unscoped().WithContext(ctx).Model(&db.JoinToken{}).
			Where("id = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?", id, now).
			Updates(map[string]any{"used_at": now, "updated_at": now})
		if res.Error == nil {
			return res.RowsAffected == 1, nil
		}
		if !isBusy(res.Error) {
			return false, res.Error
		}
		last = res.Error
	}
	return false, last
}

// classifyLoss re-reads after a lost claim race to return the most accurate
// denial: someone else redeemed, revoked, or the token just expired.
func (s *JoinTokenService) classifyLoss(ctx context.Context, id string) error {
	var tok db.JoinToken
	if err := s.deps.Store.Unscoped().WithContext(ctx).Where("id = ?", id).First(&tok).Error; err != nil {
		return ErrTokenUnknown
	}
	switch {
	case tok.RevokedAt != nil:
		return ErrTokenRevoked
	case tok.ExpiresAt != nil && !s.deps.now().Before(*tok.ExpiresAt):
		return ErrTokenExpired
	default:
		return ErrTokenRedeemed
	}
}

func isBusy(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "SQLITE_BUSY")
}

func (s *JoinTokenService) pepper(ctx context.Context) ([]byte, error) {
	if len(s.deps.Pepper) > 0 {
		return s.deps.Pepper, nil
	}
	if s.deps.Secrets == nil {
		return nil, ErrPepperMissing
	}
	v, err := s.deps.Secrets.Get(ctx, keys.JoinTokenSecret)
	if err != nil || v == "" {
		return nil, ErrPepperMissing
	}
	return []byte(v), nil
}

func tokenMAC(pepper []byte, orgID, tokenID string, exp time.Time) []byte {
	m := hmac.New(sha256.New, pepper)
	m.Write([]byte(orgID))
	m.Write([]byte("|"))
	m.Write([]byte(tokenID))
	m.Write([]byte("|"))
	m.Write([]byte(exp.UTC().Format(time.RFC3339Nano)))
	return m.Sum(nil)
}

func b64(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func parseSecret(secret string) (id string, mac []byte, ok bool) {
	rest, found := strings.CutPrefix(secret, secretPrefix)
	if !found {
		return "", nil, false
	}
	id, enc, found := strings.Cut(rest, ".")
	if !found || id == "" || enc == "" {
		return "", nil, false
	}
	mac, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil || len(mac) != sha256.Size {
		return "", nil, false
	}
	return id, mac, true
}
