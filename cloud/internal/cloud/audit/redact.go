package audit

import (
	"encoding/json"
	"strings"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/keys"
)

// MaxDetailBytes caps stored detail. Oversized detail is replaced with a
// truncation marker (see Redact) so one chatty caller cannot bloat the table.
const MaxDetailBytes = 8192

// redactedValue replaces any secret value in stored output.
const redactedValue = "[REDACTED]"

// secretSubstrings matches secret-shaped detail keys. keys.* credential names
// feed the same matcher through their last dotted segment (e.g.
// "provider.hetzner.token" matches via "token").
var secretSubstrings = []string{
	"password", "passwd", "passphrase",
	"secret", "token", "credential",
	"private", "authorization", "bearer",
	"apikey", "api_key", "auth_token",
	"join", "session_key", "webhook_secret",
}

// Redact returns a copy of e with secret-shaped detail keys removed and
// oversized detail truncated. It runs on the write path (Store.Append), never
// only on read, so secrets never reach the table.
func Redact(e Event) Event {
	if e.Detail == nil {
		return e
	}
	out := make(map[string]any, len(e.Detail))
	for k, v := range e.Detail {
		out[k] = redactValue(k, v)
	}
	if raw, err := json.Marshal(out); err == nil && len(raw) > MaxDetailBytes {
		out = map[string]any{
			"_truncated": true,
			"preview":    string(raw[:MaxDetailBytes]),
		}
	}
	e.Detail = out
	return e
}

func redactValue(key string, v any) any {
	if sensitiveKey(key) {
		return redactedValue
	}
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, subv := range t {
			m[k] = redactValue(k, subv)
		}
		return m
	case string:
		if looksLikeSecretValue(t) {
			return redactedValue
		}
		return t
	case []any:
		for i, item := range t {
			t[i] = redactValue(key, item)
		}
		return t
	default:
		return v
	}
}

// credentialSegments are the secret-shaped last dotted segments of every
// keys.* credential name (e.g. "secret_key" from "clerk.secret_key").
// Non-secret segments ("url", "endpoint", "bucket", "issuer") are excluded so
// ordinary detail keys are not over-redacted.
var credentialSegments = func() map[string]bool {
	set := map[string]bool{}
next:
	for _, name := range keys.All() {
		seg := strings.ToLower(name[strings.LastIndex(name, ".")+1:])
		for _, sub := range secretSubstrings {
			if strings.Contains(seg, sub) {
				set[seg] = true
				continue next
			}
		}
	}
	return set
}()

// sensitiveKey matches secret-shaped keys: generic secret substrings plus any
// declared credential segment. Single-word segments ("token", "url") only
// match as whole underscore/dot/dash-separated tokens so "monkey" is not
// redacted but "api_key" and "ssh_private_key" are.
func sensitiveKey(k string) bool {
	l := strings.ToLower(k)
	for _, sub := range secretSubstrings {
		if strings.Contains(l, sub) {
			return true
		}
	}
	tokens := strings.FieldsFunc(l, func(r rune) bool {
		return r == '_' || r == '.' || r == '-' || r == ':' || r == '/' || r == ' '
	})
	for _, part := range tokens {
		if part == "key" || part == "keys" || credentialSegments[part] {
			return true
		}
	}
	return false
}

// looksLikeSecretValue scrubs raw credential values even under innocuous keys:
// join secrets (ccj_<id>.<hmac>) must never be stored.
func looksLikeSecretValue(s string) bool {
	return strings.Contains(s, "ccj_")
}
