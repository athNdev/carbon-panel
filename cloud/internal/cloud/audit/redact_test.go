package audit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactSecretKeys(t *testing.T) {
	t.Parallel()
	const fake = "FAKESECRET-9f8e7d6c5b4a"
	cases := map[string]map[string]any{
		"token":            {"token": fake},
		"secret":           {"secret": fake},
		"password":         {"password": fake},
		"api_key":          {"api_key": fake},
		"authorization":    {"authorization": fake},
		"join_token":       {"join_token": fake},
		"ssh_private_key":  {"ssh_private_key": fake},
		"secret_accesskey": {"secret_access_key": fake},
		"join_secret":      {"join_secret": "ccj_abc.def"},
		"nested":           {"config": map[string]any{"password": fake}},
		"slice":            {"tokens": []any{fake}},
		"innocuous_value":  {"note": "prefix ccj_abc.def suffix"},
	}
	for name, detail := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := Redact(Event{Detail: detail})
			raw, err := json.Marshal(got.Detail)
			require.NoError(t, err)
			require.NotContains(t, string(raw), fake, "fake secret leaked for %s", name)
			if name == "join_secret" || name == "innocuous_value" {
				require.NotContains(t, string(raw), "ccj_abc.def")
			}
		})
	}
}

func TestRedactPreservesOrdinaryDetail(t *testing.T) {
	t.Parallel()
	got := Redact(Event{Detail: map[string]any{
		"monkey":   "banana",
		"endpoint": "10.0.0.1",
		"count":    3,
	}})
	require.Equal(t, "banana", got.Detail["monkey"])
	require.Equal(t, "10.0.0.1", got.Detail["endpoint"])
	require.Equal(t, 3, got.Detail["count"])
}

func TestRedactTruncatesOversizedDetail(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("x", MaxDetailBytes+1024)
	got := Redact(Event{Detail: map[string]any{"log": big}})
	require.Equal(t, true, got.Detail["_truncated"])
	raw, err := json.Marshal(got.Detail)
	require.NoError(t, err)
	require.LessOrEqual(t, len(raw), MaxDetailBytes+256)
}

func TestRedactNilDetail(t *testing.T) {
	t.Parallel()
	got := Redact(Event{})
	require.Nil(t, got.Detail)
}
