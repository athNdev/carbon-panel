package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// WebhookSubscription defines an endpoint configured to receive tenant events.
type WebhookSubscription struct {
	ID          string   `json:"id"`
	OrgID       string   `json:"org_id"`
	TargetURL   string   `json:"target_url"`
	Secret      string   `json:"secret"` // Shared secret used for HMAC-SHA256 signature
	Events      []string `json:"events"` // e.g. ["node.*", "workload.create"]
	Enabled     bool     `json:"enabled"`
	Description string   `json:"description,omitempty"`
}

// Matches returns true if the subscription matches the given event type.
func (s *WebhookSubscription) Matches(eventType string) bool {
	if !s.Enabled {
		return false
	}
	if len(s.Events) == 0 {
		return true
	}
	for _, pattern := range s.Events {
		if pattern == "*" || pattern == eventType {
			return true
		}
		if strings.HasSuffix(pattern, ".*") {
			prefix := strings.TrimSuffix(pattern, ".*")
			if strings.HasPrefix(eventType, prefix+".") {
				return true
			}
		}
	}
	return false
}

// WebhookPayload represents the standard event body delivered to target URLs.
type WebhookPayload struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	OrgID     string `json:"org_id"`
	Timestamp int64  `json:"timestamp"`
	Data      any    `json:"data"`
}

// DeliveryAttempt records the result of a single webhook dispatch.
type DeliveryAttempt struct {
	SubscriptionID string        `json:"subscription_id"`
	EventID        string        `json:"event_id"`
	StatusCode     int           `json:"status_code"`
	Duration       time.Duration `json:"duration"`
	Error          string        `json:"error,omitempty"`
	Success        bool          `json:"success"`
	AttemptedAt    time.Time     `json:"attempted_at"`
}

// SignPayload generates the HMAC-SHA256 signature string: "t=<timestamp>,v1=<hex_signature>".
func SignPayload(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.", timestamp)))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("t=%d,v1=%s", timestamp, sig)
}

// VerifySignature validates a received webhook signature header against secret and body.
func VerifySignature(secret, header string, body []byte, maxAge time.Duration) bool {
	parts := strings.Split(header, ",")
	var tStr, v1Sig string
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			tStr = kv[1]
		case "v1":
			v1Sig = kv[1]
		}
	}
	if tStr == "" || v1Sig == "" {
		return false
	}

	var ts int64
	_, err := fmt.Sscanf(tStr, "%d", &ts)
	if err != nil {
		return false
	}

	if maxAge > 0 {
		eventTime := time.Unix(ts, 0)
		if time.Since(eventTime) > maxAge || time.Until(eventTime) > 5*time.Minute {
			return false
		}
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.", ts)))
	mac.Write(body)
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expectedSig), []byte(v1Sig))
}

// Dispatcher manages webhook routing and transmission.
type Dispatcher struct {
	mu            sync.RWMutex
	httpClient    *http.Client
	subscriptions map[string]*WebhookSubscription // keyed by subscription ID
	attempts      []DeliveryAttempt
}

// NewDispatcher initializes a webhook dispatcher.
func NewDispatcher(client *http.Client) *Dispatcher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Dispatcher{
		httpClient:    client,
		subscriptions: make(map[string]*WebhookSubscription),
	}
}

// Register adds or updates a subscription.
func (d *Dispatcher) Register(sub *WebhookSubscription) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.subscriptions[sub.ID] = sub
}

// Unregister removes a subscription.
func (d *Dispatcher) Unregister(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.subscriptions, id)
}

// Dispatch sends an event to all matching subscriptions for the event's org.
func (d *Dispatcher) Dispatch(ctx context.Context, payload WebhookPayload) []DeliveryAttempt {
	d.mu.RLock()
	var targets []*WebhookSubscription
	for _, sub := range d.subscriptions {
		if sub.OrgID == payload.OrgID && sub.Matches(payload.EventType) {
			targets = append(targets, sub)
		}
	}
	d.mu.RUnlock()

	if len(targets) == 0 {
		return nil
	}

	if payload.Timestamp == 0 {
		payload.Timestamp = time.Now().Unix()
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	var results []DeliveryAttempt
	for _, target := range targets {
		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.TargetURL, bytes.NewReader(body))
		if err != nil {
			results = append(results, DeliveryAttempt{
				SubscriptionID: target.ID,
				EventID:        payload.EventID,
				Error:          err.Error(),
				AttemptedAt:    start,
			})
			continue
		}

		sig := SignPayload(target.Secret, payload.Timestamp, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "CarbonCloud-Webhook/1.0")
		req.Header.Set("X-Carbon-Signature-256", sig)

		resp, err := d.httpClient.Do(req)
		dur := time.Since(start)

		attempt := DeliveryAttempt{
			SubscriptionID: target.ID,
			EventID:        payload.EventID,
			Duration:       dur,
			AttemptedAt:    start,
		}

		if err != nil {
			attempt.Error = err.Error()
			attempt.Success = false
		} else {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			attempt.StatusCode = resp.StatusCode
			attempt.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
			if !attempt.Success {
				attempt.Error = fmt.Sprintf("HTTP status %d", resp.StatusCode)
			}
		}

		results = append(results, attempt)
	}

	d.mu.Lock()
	d.attempts = append(d.attempts, results...)
	d.mu.Unlock()

	return results
}

// RecentAttempts returns recent delivery attempts.
func (d *Dispatcher) RecentAttempts() []DeliveryAttempt {
	d.mu.RLock()
	defer d.mu.RUnlock()
	res := make([]DeliveryAttempt, len(d.attempts))
	copy(res, d.attempts)
	return res
}
