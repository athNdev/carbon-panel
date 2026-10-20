package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openNotifyDB(t *testing.T) *db.Store {
	t.Helper()
	s, err := db.Open(db.Options{
		Driver:      "sqlite",
		DSN:         filepath.Join(t.TempDir(), "notify.db"),
		AutoMigrate: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { closeStore(t, s) })
	return s
}

func closeStore(t *testing.T, s *db.Store) {
	t.Helper()
	sqlDB, err := s.DB().DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

func TestWebhook_PersistsAcrossStoreReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "notify.db")
	open := func() *db.Store {
		s, err := db.Open(db.Options{Driver: "sqlite", DSN: dsn, AutoMigrate: true})
		require.NoError(t, err)
		t.Cleanup(func() { closeStore(t, s) })
		return s
	}

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		require.NotContains(t, string(body), "join-secret-value")
		require.Contains(t, string(body), "[REDACTED]")
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	store := open()
	first := NewDispatcher(srv.Client())
	first.UseDB(store.DB())
	first.Register(&WebhookSubscription{
		ID: "sub", OrgID: "org-1", TargetURL: srv.URL, Secret: "sec", Events: []string{"*"}, Enabled: true,
	})
	first.Dispatch(ctx, WebhookPayload{
		EventID:   "evt-1",
		EventType: "node.created",
		OrgID:     "org-1",
		Data:      map[string]any{"join_token": "join-secret-value", "node_id": "n1"},
	})
	require.Equal(t, 0, hits, "enqueue must not call the tenant during the RPC")

	var pending int64
	require.NoError(t, store.DB().Model(&db.WebhookEvent{}).Where("status = ?", "pending").Count(&pending).Error)
	require.Equal(t, int64(1), pending)

	// New process: new store handle, new dispatcher, same file.
	store2 := open()
	second := NewDispatcher(srv.Client())
	second.UseDB(store2.DB())
	second.Register(&WebhookSubscription{
		ID: "sub", OrgID: "org-1", TargetURL: srv.URL, Secret: "sec", Events: []string{"*"}, Enabled: true,
	})
	sent, err := second.Relay(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, sent)
	require.Equal(t, 1, hits)

	var row db.WebhookEvent
	require.NoError(t, store2.DB().First(&row, "id = ?", "evt-1").Error)
	require.Equal(t, "sent", row.Status)
	require.NotContains(t, row.Payload, "join-secret-value")
}

func TestWebhook_RollbackLeavesNoRow(t *testing.T) {
	store := openNotifyDB(t)
	d := NewDispatcher(nil)
	d.UseDB(store.DB())
	err := store.DB().Transaction(func(tx *gorm.DB) error {
		d.Dispatch(ContextWithDB(context.Background(), tx), WebhookPayload{
			EventID: "evt-roll", EventType: "org.invite.created", OrgID: "org-1",
			Data: map[string]any{"email": "a@b.c"},
		})
		return errors.New("rollback")
	})
	require.Error(t, err)
	var n int64
	require.NoError(t, store.DB().Model(&db.WebhookEvent{}).Count(&n).Error)
	require.Equal(t, int64(0), n)
}

func TestEmail_PersistsAcrossStoreReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "mail.db")
	open := func() *db.Store {
		s, err := db.Open(db.Options{Driver: "sqlite", DSN: dsn, AutoMigrate: true})
		require.NoError(t, err)
		t.Cleanup(func() { closeStore(t, s) })
		return s
	}
	store := open()
	sender := NewFakeEmailSender()
	box := NewOutbox(sender)
	box.UseDB(store.DB())
	box.Enqueue("org-1", "a@b.c", "hi", "body", "")
	require.Equal(t, 1, box.PendingCount())
	require.Empty(t, sender.Sent)

	store2 := open()
	sender2 := NewFakeEmailSender()
	box2 := NewOutbox(sender2)
	box2.UseDB(store2.DB())
	sent, err := box2.Flush(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, sent)
	require.Len(t, sender2.Sent, 1)
	require.Equal(t, 0, box2.PendingCount())
}

func TestWebhook_RedactedPayloadStored(t *testing.T) {
	store := openNotifyDB(t)
	d := NewDispatcher(nil)
	d.UseDB(store.DB())
	d.Dispatch(context.Background(), WebhookPayload{
		EventID: "evt-redact", EventType: "node.joined", OrgID: "org-1",
		Data: map[string]any{"join_token": "super-secret", "name": "edge"},
	})
	var row db.WebhookEvent
	require.NoError(t, store.DB().First(&row, "id = ?", "evt-redact").Error)
	var payload WebhookPayload
	require.NoError(t, json.Unmarshal([]byte(row.Payload), &payload))
	data, ok := payload.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "[REDACTED]", data["join_token"])
	require.Equal(t, "edge", data["name"])
}
