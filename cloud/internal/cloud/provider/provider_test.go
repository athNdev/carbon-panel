package provider

import (
	"context"
	"testing"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/secrets"
	"github.com/stretchr/testify/require"
)

func TestRegistryListsSix(t *testing.T) {
	t.Parallel()
	got := List()
	require.Len(t, got, 6)
	names := map[string]bool{}
	for _, d := range got {
		names[d.Name] = true
		require.NotEmpty(t, d.Title)
		require.NotEmpty(t, d.Regions)
		require.NotEmpty(t, d.CredentialKeys)
	}
	for _, want := range []string{"hetzner", "aws", "gcp", "digitalocean", "proxmox", "generic"} {
		require.True(t, names[want], "missing provider %s", want)
	}
}

func TestGetUnknown(t *testing.T) {
	t.Parallel()
	_, ok := Get("nope")
	require.False(t, ok)
}

func TestUncredentialedUnavailableWithMissingKeys(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Empty env prefix forces every lookup to miss.
	r := secrets.NewResolver(secrets.NewEnv("CARBONCLOUD_W2PROVISION_EMPTY_"))
	for _, d := range List() {
		require.False(t, d.Available(ctx, r), d.Name)
		missing := d.MissingKeys(ctx, r)
		require.Equal(t, d.CredentialKeys, missing)
	}
	reg := NewRegistry(r)
	for _, d := range List() {
		require.False(t, reg.Available(ctx, d.Name))
		require.NotEmpty(t, reg.MissingKeys(ctx, d.Name))
	}
	require.False(t, reg.Available(ctx, "nope"))
	require.Nil(t, reg.MissingKeys(ctx, "nope"))
}

func TestRegionMembership(t *testing.T) {
	t.Parallel()
	d, ok := Get("aws")
	require.True(t, ok)
	require.True(t, d.HasRegion("us-east-1"))
	require.False(t, d.HasRegion("fsn1"))
	g, ok := Get("generic")
	require.True(t, ok)
	require.True(t, g.HasRegion("onprem"))
}

func TestNilResolverMeansUnavailable(t *testing.T) {
	t.Parallel()
	d, ok := Get("hetzner")
	require.True(t, ok)
	require.False(t, d.Available(context.Background(), nil))
	require.Equal(t, d.CredentialKeys, d.MissingKeys(context.Background(), nil))
}
