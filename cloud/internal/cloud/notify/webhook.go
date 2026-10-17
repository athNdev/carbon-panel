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

// DeadLetter records an event delivery that permanently failed after all retry attempts.
type DeadLetter struct {
	SubscriptionID string    `json:"subscription_id"`
	EventID        string    `json:"event_id"`
	EventType      string    `json:"event_type"`
	TargetURL      string    `json:"target_url"`
	Attempts       int       `json:"attempts"`
	LastError      string    `json:"last_error"`
	FailedAt       time.Time `json:"failed_at"`
}

// SignPayload generates the HMAC-SHA256 signature string: "t=<timestamp>,v1=<hex_signature>".
func SignPayload(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", timestamp)
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
	_, _ = fmt.Fprintf(mac, "%d.", ts)
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
	deadLetters   []DeadLetter
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

// Dispatch sends an event to all matching subscriptions for the event's org with a single attempt.
func (d *Dispatcher) Dispatch(ctx context.Context, payload WebhookPayload) []DeliveryAttempt {
	return d.DispatchWithRetries(ctx, payload, 0, 0)
}

// DispatchWithRetries sends an event to all matching subscriptions for the event's org,
// retrying transient errors (network errors, 429 Too Many Requests, and HTTP >= 500)
// up to maxRetries times with exponential backoff. Permanently failed deliveries are recorded in DLQ.
func (d *Dispatcher) DispatchWithRetries(ctx context.Context, payload WebhookPayload, maxRetries int, initialBackoff time.Duration) []DeliveryAttempt {
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

	if initialBackoff <= 0 {
		initialBackoff = 20 * time.Millisecond
	}

	var results []DeliveryAttempt
	var deadLetters []DeadLetter

	for _, target := range targets {
		sig := SignPayload(target.Secret, payload.Timestamp, body)
		attemptCount := 0
		var lastAttempt DeliveryAttempt
		currentBackoff := initialBackoff

	retryLoop:
		for {
			attemptCount++
			start := time.Now()
			req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, target.TargetURL, bytes.NewReader(body))
			if reqErr != nil {
				lastAttempt = DeliveryAttempt{
					SubscriptionID: target.ID,
					EventID:        payload.EventID,
					Error:          reqErr.Error(),
					AttemptedAt:    start,
					Success:        false,
				}
				break
			}

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "CarbonCloud-Webhook/1.0")
			req.Header.Set("X-Carbon-Signature-256", sig)

			resp, doErr := d.httpClient.Do(req)
			dur := time.Since(start)

			lastAttempt = DeliveryAttempt{
				SubscriptionID: target.ID,
				EventID:        payload.EventID,
				Duration:       dur,
				AttemptedAt:    start,
			}

			retryable := false
			if doErr != nil {
				lastAttempt.Error = doErr.Error()
				lastAttempt.Success = false
				retryable = true
			} else {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				lastAttempt.StatusCode = resp.StatusCode
				lastAttempt.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
				if !lastAttempt.Success {
					lastAttempt.Error = fmt.Sprintf("HTTP status %d", resp.StatusCode)
					if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
						retryable = true
					}
				}
			}

			if lastAttempt.Success || !retryable || attemptCount > maxRetries {
				break
			}

			// Backoff before retry
			select {
			case <-ctx.Done():
				lastAttempt.Error = ctx.Err().Error()
				lastAttempt.Success = false
				break retryLoop
			case <-time.After(currentBackoff):
				currentBackoff *= 2
				if currentBackoff > 2*time.Second {
					currentBackoff = 2 * time.Second
				}
			}
		}

		results = append(results, lastAttempt)

		if !lastAttempt.Success {
			deadLetters = append(deadLetters, DeadLetter{
				SubscriptionID: target.ID,
				EventID:        payload.EventID,
				EventType:      payload.EventType,
				TargetURL:      target.TargetURL,
				Attempts:       attemptCount,
				LastError:      lastAttempt.Error,
				FailedAt:       time.Now().UTC(),
			})
		}
	}

	d.mu.Lock()
	d.attempts = append(d.attempts, results...)
	if len(deadLetters) > 0 {
		d.deadLetters = append(d.deadLetters, deadLetters...)
	}
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

// DeadLetters returns recorded dead-letter deliveries that failed permanently.
func (d *Dispatcher) DeadLetters() []DeadLetter {
	d.mu.RLock()
	defer d.mu.RUnlock()
	res := make([]DeadLetter, len(d.deadLetters))
	copy(res, d.deadLetters)
	return res
}

// ClearDeadLetters clears the dead-letter list.
func (d *Dispatcher) ClearDeadLetters() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deadLetters = nil
}
