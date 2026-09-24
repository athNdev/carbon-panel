package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOutbox_EnqueueAndFlush(t *testing.T) {
	ctx := context.Background()
	sender := NewFakeEmailSender()
	outbox := NewOutbox(sender)

	// Enqueue 2 messages
	msg1 := outbox.Enqueue("org_1", "dev1@example.com", "Welcome", "Welcome to Carbon Cloud", "")
	msg2 := outbox.Enqueue("org_1", "dev2@example.com", "Node Online", "Your node is online", "")
	require.Equal(t, 2, outbox.PendingCount())

	// Flush all
	sent, err := outbox.Flush(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 2, sent)
	require.Equal(t, 0, outbox.PendingCount())

	// Verify delivered messages
	require.Len(t, sender.Sent, 2)
	require.Equal(t, msg1.Recipient, sender.Sent[0].Recipient)
	require.Equal(t, msg2.Recipient, sender.Sent[1].Recipient)

	// Verify history
	history := outbox.History()
	require.Len(t, history, 2)
	require.Equal(t, EmailStatusSent, history[0].Status)
	require.NotNil(t, history[0].SentAt)
}

func TestOutbox_RetryOnFailure(t *testing.T) {
	ctx := context.Background()
	sender := NewFakeEmailSender()
	outbox := NewOutbox(sender)

	outbox.Enqueue("org_1", "fail@example.com", "Alert", "Test alert", "")
	require.Equal(t, 1, outbox.PendingCount())

	// Configure sender to fail first attempt
	sender.FailNext = true
	sent, err := outbox.Flush(ctx, 10)
	require.Error(t, err)
	require.Equal(t, 0, sent)
	// Should be re-queued because attempts < 3
	require.Equal(t, 1, outbox.PendingCount())

	// Second attempt succeeds
	sent, err = outbox.Flush(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, sent)
	require.Equal(t, 0, outbox.PendingCount())
	require.Len(t, sender.Sent, 1)
}

func TestWebhook_HMACSignatureVerification(t *testing.T) {
	secret := "whsec_test_secret_key_12345"
	now := time.Now().Unix()
	body := []byte(`{"event_id":"evt_1","event_type":"node.created","org_id":"org_1"}`)

	sigHeader := SignPayload(secret, now, body)
	require.Contains(t, sigHeader, "t=")
	require.Contains(t, sigHeader, "v1=")

	// Successful verification
	valid := VerifySignature(secret, sigHeader, body, 5*time.Minute)
	require.True(t, valid)

	// Tampered body fails verification
	tamperedBody := []byte(`{"event_id":"evt_1","event_type":"node.deleted","org_id":"org_1"}`)
	valid = VerifySignature(secret, sigHeader, tamperedBody, 5*time.Minute)
	require.False(t, valid)

	// Wrong secret fails verification
	valid = VerifySignature("wrong_secret", sigHeader, body, 5*time.Minute)
	require.False(t, valid)

	// Expired signature fails verification
	oldTimestamp := time.Now().Add(-10 * time.Minute).Unix()
	expiredSigHeader := SignPayload(secret, oldTimestamp, body)
	valid = VerifySignature(secret, expiredSigHeader, body, 5*time.Minute)
	require.False(t, valid)
}

func TestWebhook_Dispatcher(t *testing.T) {
	var receivedBody []byte
	var receivedSig string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-Carbon-Signature-256")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := NewDispatcher(server.Client())
	sub := &WebhookSubscription{
		ID:        "sub_1",
		OrgID:     "org_abc",
		TargetURL: server.URL,
		Secret:    "super_secret_key",
		Events:    []string{"node.*", "workload.created"},
		Enabled:   true,
	}
	dispatcher.Register(sub)

	t.Run("matching event dispatches and verifies", func(t *testing.T) {
		payload := WebhookPayload{
			EventID:   "evt_node_100",
			EventType: "node.online",
			OrgID:     "org_abc",
			Data:      map[string]string{"node_id": "node_999"},
		}
		attempts := dispatcher.Dispatch(context.Background(), payload)
		require.Len(t, attempts, 1)
		require.True(t, attempts[0].Success)
		require.Equal(t, 200, attempts[0].StatusCode)

		// Verify signature received by the server
		valid := VerifySignature(sub.Secret, receivedSig, receivedBody, 1*time.Minute)
		require.True(t, valid)
	})

	t.Run("non-matching event type is ignored", func(t *testing.T) {
		payload := WebhookPayload{
			EventID:   "evt_other",
			EventType: "user.login",
			OrgID:     "org_abc",
		}
		attempts := dispatcher.Dispatch(context.Background(), payload)
		require.Empty(t, attempts)
	})

	t.Run("different org is ignored", func(t *testing.T) {
		payload := WebhookPayload{
			EventID:   "evt_other_org",
			EventType: "node.online",
			OrgID:     "org_different",
		}
		attempts := dispatcher.Dispatch(context.Background(), payload)
		require.Empty(t, attempts)
	})
}

func TestWebhook_RetriesAndDeadLetter(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := NewDispatcher(server.Client())
	sub := &WebhookSubscription{
		ID:        "sub_retry",
		OrgID:     "org_1",
		TargetURL: server.URL,
		Secret:    "secret_123",
		Events:    []string{"node.*"},
		Enabled:   true,
	}
	dispatcher.Register(sub)

	t.Run("retries transient 500 error and eventually succeeds", func(t *testing.T) {
		callCount = 0
		payload := WebhookPayload{
			EventID:   "evt_retry_1",
			EventType: "node.online",
			OrgID:     "org_1",
		}
		attempts := dispatcher.DispatchWithRetries(context.Background(), payload, 3, 5*time.Millisecond)
		require.Len(t, attempts, 1)
		require.True(t, attempts[0].Success)
		require.Equal(t, 200, attempts[0].StatusCode)
		require.Equal(t, 3, callCount)
		require.Empty(t, dispatcher.DeadLetters())
	})

	t.Run("exhausts retries on persistent failure and routes to dead letter queue", func(t *testing.T) {
		callCount = 0
		payload := WebhookPayload{
			EventID:   "evt_retry_fail",
			EventType: "node.online",
			OrgID:     "org_1",
		}
		attempts := dispatcher.DispatchWithRetries(context.Background(), payload, 1, 5*time.Millisecond)
		require.Len(t, attempts, 1)
		require.False(t, attempts[0].Success)
		require.Equal(t, 500, attempts[0].StatusCode)
		require.Equal(t, 2, callCount)

		dlq := dispatcher.DeadLetters()
		require.Len(t, dlq, 1)
		require.Equal(t, "sub_retry", dlq[0].SubscriptionID)
		require.Equal(t, "evt_retry_fail", dlq[0].EventID)
		require.Equal(t, 2, dlq[0].Attempts)
		require.Contains(t, dlq[0].LastError, "HTTP status 500")

		dispatcher.ClearDeadLetters()
		require.Empty(t, dispatcher.DeadLetters())
	})

	t.Run("non-retryable client error does not retry", func(t *testing.T) {
		clientErrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer clientErrServer.Close()

		subBad := &WebhookSubscription{
			ID:        "sub_bad",
			OrgID:     "org_bad",
			TargetURL: clientErrServer.URL,
			Secret:    "secret_123",
			Events:    []string{"node.*"},
			Enabled:   true,
		}
		dispatcher.Register(subBad)

		callCount = 0
		payload := WebhookPayload{
			EventID:   "evt_bad_req",
			EventType: "node.online",
			OrgID:     "org_bad",
		}
		attempts := dispatcher.DispatchWithRetries(context.Background(), payload, 3, 5*time.Millisecond)
		require.Len(t, attempts, 1)
		require.False(t, attempts[0].Success)
		require.Equal(t, 400, attempts[0].StatusCode)
		require.Equal(t, 1, callCount)
		require.Len(t, dispatcher.DeadLetters(), 1)
	})
}
