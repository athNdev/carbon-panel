package keys

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCapabilities(t *testing.T) {
	caps := Capabilities()
	require.NotEmpty(t, caps)
	require.True(t, sort.StringsAreSorted(caps))
	require.Contains(t, caps, CapClerk)
	require.Contains(t, caps, CapProvisioning)
	require.Contains(t, caps, CapStateBackend)
	require.Contains(t, caps, CapNodeIdentity)
	require.Contains(t, caps, CapTenantCredentials)
	require.Contains(t, caps, CapEmail)
}

func TestRequiredFor(t *testing.T) {
	t.Run("clerk capability", func(t *testing.T) {
		req := RequiredFor(CapClerk)
		require.Equal(t, []string{ClerkIssuer}, req)
		require.True(t, sort.StringsAreSorted(req))
	})

	t.Run("node identity capability", func(t *testing.T) {
		req := RequiredFor(CapNodeIdentity)
		require.Equal(t, []string{NodeCACert, NodeCAKey, JoinTokenSecret}, req)
		require.True(t, sort.StringsAreSorted(req))
	})

	t.Run("unknown capability", func(t *testing.T) {
		req := RequiredFor("nonexistent.capability")
		require.Empty(t, req)
	})
}

func TestAll(t *testing.T) {
	allKeys := All()
	require.NotEmpty(t, allKeys)
	require.True(t, sort.StringsAreSorted(allKeys))

	// Ensure no duplicate keys in All()
	seen := make(map[string]bool)
	for _, k := range allKeys {
		require.False(t, seen[k], "duplicate key in All(): %s", k)
		seen[k] = true
	}

	// Ensure every capability's required keys are included in All()
	for _, c := range Capabilities() {
		for _, req := range RequiredFor(c) {
			require.Contains(t, allKeys, req, "capability %s requires %s which is missing in All()", c, req)
		}
	}
}

func TestLooksLikePlaceholder(t *testing.T) {
	placeholders := []string{
		"",
		"   ",
		"changeme",
		"CHANGEME",
		"Change_Me",
		"some-placeholder-val",
		"TODO: fill this in",
		"xxx",
		"your-api-key",
		"<ENTER_KEY_HERE>",
		"${AWS_SECRET_ACCESS_KEY}",
	}

	for _, p := range placeholders {
		require.True(t, LooksLikePlaceholder(p), "expected %q to look like placeholder", p)
	}

	validKeys := []string{
		"cca_live_9834275098234759",
		"sk_test_51Mz492049284092",
		"ghp_092384092384029384092384",
		"postgres://user:pass@localhost:5432/db",
		"https://clerk.carbon.internal",
	}

	for _, v := range validKeys {
		require.False(t, LooksLikePlaceholder(v), "expected %q to NOT look like placeholder", v)
	}
}
