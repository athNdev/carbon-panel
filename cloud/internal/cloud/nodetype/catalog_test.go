package nodetype

import (
	"path/filepath"
	"testing"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/stretchr/testify/require"
)

func openStore(t *testing.T) *db.Store {
	t.Helper()
	s, err := db.Open(db.Options{
		Driver:      "sqlite",
		DSN:         filepath.Join(t.TempDir(), "test.db"),
		AutoMigrate: true,
	})
	require.NoError(t, err)
	require.NoError(t, s.SeedDefaults(t.Context()))
	return s
}

func TestSeedIsIdempotent(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	c := NewCatalog(s)
	require.NoError(t, c.EnsureSeeded(t.Context()))
	require.NoError(t, c.EnsureSeeded(t.Context()))

	got, err := c.List(t.Context())
	require.NoError(t, err)
	require.Len(t, got, 5)
	want := []string{"nano", "small", "medium", "large", "xlarge"}
	for i, id := range want {
		require.Equal(t, id, got[i].ID)
	}
}

func TestSeedCoversAllProviders(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	c := NewCatalog(s)
	got, err := c.List(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, got)
	for _, nt := range got {
		m := nt.InstanceTypeMap()
		for _, p := range Providers {
			require.NotEmpty(t, m[p], "node type %s missing provider %s", nt.ID, p)
		}
	}
}

func TestGetAndInstanceType(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	c := NewCatalog(s)

	nt, err := c.Get(t.Context(), "medium")
	require.NoError(t, err)
	require.Equal(t, 4, nt.VCPU)

	_, err = c.Get(t.Context(), "nope")
	require.ErrorIs(t, err, ErrNodeTypeNotFound)

	inst, ok := c.InstanceType(t.Context(), "medium", "hetzner")
	require.True(t, ok)
	require.NotEmpty(t, inst)

	_, ok = c.InstanceType(t.Context(), "medium", "nope")
	require.False(t, ok)
	_, ok = c.InstanceType(t.Context(), "nope", "hetzner")
	require.False(t, ok)
}

func TestUpsertCustomType(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	c := NewCatalog(s)

	custom := db.NodeType{ID: "custom-1", Name: "Custom", VCPU: 3, RAMMB: 6144, DiskGB: 60, SortOrder: 15, Enabled: true}
	custom.SetInstanceTypes(map[string]string{"generic": "custom"})
	got, err := c.Upsert(t.Context(), custom)
	require.NoError(t, err)
	require.Equal(t, "custom-1", got.ID)

	back, err := c.Get(t.Context(), "custom-1")
	require.NoError(t, err)
	require.Equal(t, 3, back.VCPU)

	_, err = c.Upsert(t.Context(), db.NodeType{})
	require.ErrorIs(t, err, ErrNodeTypeNotFound)
}

func TestEnabledFiltersDisabled(t *testing.T) {
	t.Parallel()
	s := openStore(t)
	c := NewCatalog(s)

	nt, err := c.Get(t.Context(), "nano")
	require.NoError(t, err)
	nt.Enabled = false
	_, err = c.Upsert(t.Context(), nt)
	require.NoError(t, err)

	enabled, err := c.Enabled(t.Context())
	require.NoError(t, err)
	require.Len(t, enabled, 4)
	for _, e := range enabled {
		require.NotEqual(t, "nano", e.ID)
	}
}
