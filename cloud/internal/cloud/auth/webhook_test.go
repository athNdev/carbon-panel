package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testSecret(t *testing.T) (b64 string, raw []byte) {
	t.Helper()
	raw = make([]byte, 32)
	_, err := rand.Read(raw)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(raw), raw
}

func signWebhook(t *testing.T, rawSecret []byte, id, ts string, body []byte) http.Header {
	t.Helper()
	mac := hmac.New(sha256.New, rawSecret)
	mac.Write([]byte(id + "." + ts + "." + string(body)))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	h := http.Header{}
	h.Set("svix-id", id)
	h.Set("svix-timestamp", ts)
	h.Set("svix-signature", "v1,"+sig)
	return h
}

func webhookCfg(secret string) WebhookConfig {
	return WebhookConfig{Secret: secret, Tolerance: 5 * time.Minute}
}

const orgCreatedBody = `{"data":{"id":"org_1","name":"Acme","slug":"acme","object":"organization"},"object":"event","type":"organization.created"}`

func TestWebhookValid(t *testing.T) {
	t.Parallel()
	b64, raw := testSecret(t)
	body := []byte(orgCreatedBody)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	h := signWebhook(t, raw, "msg_1", ts, body)

	ev, err := VerifyWebhook(webhookCfg(b64), h, body)
	require.NoError(t, err)
	require.Equal(t, "organization.created", ev.Type)
	require.Equal(t, "msg_1", ev.ID)
	require.WithinDuration(t, time.Now(), ev.Timestamp, 2*time.Minute)
	require.JSONEq(t, orgCreatedBody, string(ev.Payload))

	org, err := ev.Organization()
	require.NoError(t, err)
	require.Equal(t, "org_1", org.ID)
	require.Equal(t, "Acme", org.Name)
	require.Equal(t, "acme", org.Slug)
}

func TestWebhookRejections(t *testing.T) {
	t.Parallel()
	b64, raw := testSecret(t)
	body := []byte(orgCreatedBody)
	now := strconv.FormatInt(time.Now().Unix(), 10)

	t.Run("tampered body", func(t *testing.T) {
		t.Parallel()
		h := signWebhook(t, raw, "msg_1", now, body)
		_, err := VerifyWebhook(webhookCfg(b64), h, []byte(`{"data":{},"object":"event","type":"organization.created"}`))
		require.ErrorIs(t, err, ErrWebhookSignature)
	})
	t.Run("stale timestamp", func(t *testing.T) {
		t.Parallel()
		old := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
		h := signWebhook(t, raw, "msg_2", old, body)
		_, err := VerifyWebhook(webhookCfg(b64), h, body)
		require.ErrorIs(t, err, ErrWebhookTimestamp)
	})
	t.Run("future timestamp", func(t *testing.T) {
		t.Parallel()
		future := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
		h := signWebhook(t, raw, "msg_3", future, body)
		_, err := VerifyWebhook(webhookCfg(b64), h, body)
		require.ErrorIs(t, err, ErrWebhookTimestamp)
	})
	t.Run("wrong secret", func(t *testing.T) {
		t.Parallel()
		wrong := make([]byte, 32)
		_, _ = rand.Read(wrong)
		h := signWebhook(t, wrong, "msg_4", now, body)
		_, err := VerifyWebhook(webhookCfg(b64), h, body)
		require.ErrorIs(t, err, ErrWebhookSignature)
	})
	t.Run("missing headers", func(t *testing.T) {
		t.Parallel()
		_, err := VerifyWebhook(webhookCfg(b64), http.Header{}, body)
		require.ErrorIs(t, err, ErrWebhookHeaders)
	})
	t.Run("missing signature entry", func(t *testing.T) {
		t.Parallel()
		h := signWebhook(t, raw, "msg_5", now, body)
		h.Del("svix-signature")
		_, err := VerifyWebhook(webhookCfg(b64), h, body)
		require.ErrorIs(t, err, ErrWebhookHeaders)
	})
	t.Run("bad secret", func(t *testing.T) {
		t.Parallel()
		h := signWebhook(t, raw, "msg_6", now, body)
		_, err := VerifyWebhook(WebhookConfig{Secret: "!!!not-base64!!!"}, h, body)
		require.ErrorIs(t, err, ErrWebhookSecret)
	})
	t.Run("no type in body", func(t *testing.T) {
		t.Parallel()
		empty := []byte(`{"data":{}}`)
		h := signWebhook(t, raw, "msg_7", now, empty)
		_, err := VerifyWebhook(webhookCfg(b64), h, empty)
		require.ErrorIs(t, err, ErrWebhookHeaders)
	})
}

func TestWebhookUnknownTypePreserved(t *testing.T) {
	t.Parallel()
	b64, raw := testSecret(t)
	body := []byte(`{"data":{"mystery":42},"object":"event","type":"something.new"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	h := signWebhook(t, raw, "msg_9", ts, body)

	ev, err := VerifyWebhook(webhookCfg(b64), h, body)
	require.NoError(t, err, "unknown types must not fail")
	require.Equal(t, "something.new", ev.Type)
	require.JSONEq(t, string(body), string(ev.Payload))
}

func TestWebhookPrefixAndTolerance(t *testing.T) {
	t.Parallel()
	b64, raw := testSecret(t)
	body := []byte(orgCreatedBody)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	h := signWebhook(t, raw, "msg_10", ts, body)

	// whsec_ prefix tolerated.
	ev, err := VerifyWebhook(webhookCfg("whsec_"+b64), h, body)
	require.NoError(t, err)
	require.Equal(t, "organization.created", ev.Type)

	// Custom clock inside tolerance.
	cfg := WebhookConfig{Secret: b64, Tolerance: time.Minute, Clock: func() time.Time { return time.Now() }}
	_, err = VerifyWebhook(cfg, h, body)
	require.NoError(t, err)
}

func TestWebhookMembership(t *testing.T) {
	t.Parallel()
	b64, raw := testSecret(t)
	body := []byte(`{"data":{"id":"m_1","organization":{"id":"org_9"},"public_user_data":{"user_id":"user_7"},"role":"org:admin"},"object":"event","type":"organizationMembership.created"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	h := signWebhook(t, raw, "msg_11", ts, body)

	ev, err := VerifyWebhook(webhookCfg(b64), h, body)
	require.NoError(t, err)
	m, err := ev.Membership()
	require.NoError(t, err)
	require.Equal(t, "m_1", m.ID)
	require.Equal(t, "org_9", m.OrganizationID)
	require.Equal(t, "user_7", m.UserID)
	require.Equal(t, "org:admin", m.Role)

	// Flat variant.
	flat := []byte(`{"data":{"id":"m_2","organization_id":"org_3","user_id":"user_4","role_name":"org:member"},"object":"event","type":"organizationMembership.updated"}`)
	h2 := signWebhook(t, raw, "msg_12", ts, flat)
	ev2, err := VerifyWebhook(webhookCfg(b64), h2, flat)
	require.NoError(t, err)
	m2, err := ev2.Membership()
	require.NoError(t, err)
	require.Equal(t, "org_3", m2.OrganizationID)
	require.Equal(t, "user_4", m2.UserID)
	require.Equal(t, "org:member", m2.Role)
}

func TestWebhookMultipleSignatures(t *testing.T) {
	t.Parallel()
	b64, raw := testSecret(t)
	body := []byte(orgCreatedBody)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	h := signWebhook(t, raw, "msg_13", ts, body)
	// Rotation format: stale entry first, valid entry second.
	h.Set("svix-signature", "v1,AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA "+h.Get("svix-signature"))
	_, err := VerifyWebhook(webhookCfg(b64), h, body)
	require.NoError(t, err)
}
