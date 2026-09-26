package principal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrincipal_Anonymous(t *testing.T) {
	require.True(t, Principal{}.Anonymous())

	require.False(t, Principal{Kind: KindSession}.Anonymous())
	require.False(t, Principal{UserID: "usr_123"}.Anonymous())
	require.False(t, Principal{NodeID: "node_123"}.Anonymous())
}

func TestPrincipal_Has(t *testing.T) {
	p := Principal{
		Permissions: []string{"nodes.read", "nodes.write", "workloads.read"},
	}

	require.True(t, p.Has("nodes.read"))
	require.True(t, p.Has("nodes.write"))
	require.True(t, p.Has("workloads.read"))
	require.False(t, p.Has("workloads.write"))
	require.False(t, p.Has("billing.manage"))
}

func TestContextHelpers(t *testing.T) {
	ctx := context.Background()

	// Empty context
	p, ok := From(ctx)
	require.False(t, ok)
	require.True(t, p.Anonymous())
	require.Empty(t, OrgID(ctx))

	// Context with principal
	expected := Principal{
		Kind:        KindSession,
		UserID:      "usr_abc",
		OrgID:       "org_xyz",
		Role:        "owner",
		Email:       "alice@example.com",
		Permissions: []string{"*"},
	}

	child := WithPrincipal(ctx, expected)
	actual, ok := From(child)
	require.True(t, ok)
	require.Equal(t, expected, actual)
	require.Equal(t, "org_xyz", OrgID(child))
}
