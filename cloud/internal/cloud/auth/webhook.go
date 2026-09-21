package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// defaultWebhookTolerance bounds webhook timestamps (replay protection).
const defaultWebhookTolerance = 5 * time.Minute

// Clerk webhook event types the org sync understands. Unknown types are kept
// as raw payload and never fail verification.
const (
	EventOrganizationCreated = "organization.created"
	EventOrganizationUpdated = "organization.updated"
	EventOrganizationDeleted = "organization.deleted"
	EventMembershipCreated   = "organizationMembership.created"
	EventMembershipUpdated   = "organizationMembership.updated"
	EventMembershipDeleted   = "organizationMembership.deleted"
	EventUserDeleted         = "user.deleted"
)

// WebhookConfig configures Svix-scheme webhook verification.
type WebhookConfig struct {
	// Secret is the Svix signing secret, base64-encoded, with or without the
	// "whsec_" prefix.
	Secret string
	// Tolerance bounds the svix-timestamp skew; 0 = 5m.
	Tolerance time.Duration
	// Clock optionally overrides time.Now (tests).
	Clock func() time.Time
}

// WebhookEvent is a verified Clerk webhook delivery.
type WebhookEvent struct {
	Type      string // e.g. "organization.created"
	Timestamp time.Time
	ID        string
	Payload   json.RawMessage
}

// OrganizationData is the subset of an organization.* payload the org sync
// needs. Fields absent from the delivery stay empty.
type OrganizationData struct {
	ID   string
	Name string
	Slug string
}

// MembershipData is the subset of an organizationMembership.* payload the org
// sync needs. Clerk nests the org and user ids; the accessors below look in
// both the nested and flat positions.
type MembershipData struct {
	ID             string
	OrganizationID string
	UserID         string
	Role           string
}

// VerifyWebhook verifies the Svix signature scheme and returns the event.
// Signed content is "{svix-id}.{svix-timestamp}.{body}"; the signature is
// base64(HMAC-SHA256(base64decode(secret), content)) compared in constant
// time against each space-separated "v1,<sig>" entry.
func VerifyWebhook(cfg WebhookConfig, headers http.Header, body []byte) (*WebhookEvent, error) {
	key, err := decodeWebhookSecret(cfg.Secret)
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(headers.Get("svix-id"))
	ts := strings.TrimSpace(headers.Get("svix-timestamp"))
	sigs := strings.TrimSpace(headers.Get("svix-signature"))
	if id == "" || ts == "" || sigs == "" {
		return nil, ErrWebhookHeaders
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: bad svix-timestamp: %v", ErrWebhookHeaders, err)
	}
	now := time.Now
	if cfg.Clock != nil {
		now = cfg.Clock
	}
	tol := cfg.Tolerance
	if tol <= 0 {
		tol = defaultWebhookTolerance
	}
	if skew := now().Sub(time.Unix(unix, 0)); skew > tol || skew < -tol {
		return nil, ErrWebhookTimestamp
	}

	signed := id + "." + ts + "." + string(body)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signed))
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	matched := false
	for _, entry := range strings.Fields(sigs) {
		sig, ok := strings.CutPrefix(entry, "v1,")
		if !ok || len(sig) != len(want) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(sig), []byte(want)) == 1 {
			matched = true
			break
		}
	}
	if !matched {
		return nil, ErrWebhookSignature
	}

	var envelope struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&envelope); err != nil || envelope.Type == "" {
		return nil, fmt.Errorf("%w: body has no event type", ErrWebhookHeaders)
	}
	out := &WebhookEvent{
		Type:      envelope.Type,
		Timestamp: time.Unix(unix, 0).UTC(),
		ID:        id,
		Payload:   append(json.RawMessage(nil), body...),
	}
	return out, nil
}

// decodeWebhookSecret strips an optional "whsec_" prefix and base64-decodes
// the remainder, accepting padded and unpadded encodings.
func decodeWebhookSecret(secret string) ([]byte, error) {
	s := strings.TrimSpace(secret)
	s = strings.TrimPrefix(s, "whsec_")
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
		if key, err := enc.DecodeString(s); err == nil {
			return key, nil
		}
	}
	return nil, ErrWebhookSecret
}

// Organization extracts the org fields from an organization.* event payload.
// It fails only when the payload itself is undecodable, never on unknown
// event types.
func (e *WebhookEvent) Organization() (OrganizationData, error) {
	var envelope struct {
		Data struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"data"`
	}
	if err := json.Unmarshal(e.Payload, &envelope); err != nil {
		return OrganizationData{}, fmt.Errorf("%w: %v", ErrWebhookHeaders, err)
	}
	return OrganizationData{ID: envelope.Data.ID, Name: envelope.Data.Name, Slug: envelope.Data.Slug}, nil
}

// Membership extracts the membership fields from an organizationMembership.*
// event payload, looking for org/user ids in both nested and flat positions.
func (e *WebhookEvent) Membership() (MembershipData, error) {
	var envelope struct {
		Data struct {
			ID             string `json:"id"`
			Role           string `json:"role"`
			RoleName       string `json:"role_name"`
			OrganizationID string `json:"organization_id"`
			Organization   struct {
				ID string `json:"id"`
			} `json:"organization"`
			UserID         string `json:"user_id"`
			PublicUserData struct {
				UserID string `json:"user_id"`
			} `json:"public_user_data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(e.Payload, &envelope); err != nil {
		return MembershipData{}, fmt.Errorf("%w: %v", ErrWebhookHeaders, err)
	}
	d := envelope.Data
	out := MembershipData{ID: d.ID, Role: d.Role}
	if out.Role == "" {
		out.Role = d.RoleName
	}
	out.OrganizationID = d.OrganizationID
	if out.OrganizationID == "" {
		out.OrganizationID = d.Organization.ID
	}
	out.UserID = d.UserID
	if out.UserID == "" {
		out.UserID = d.PublicUserData.UserID
	}
	return out, nil
}
