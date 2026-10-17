package db

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPostgresMigrateAndScope runs the same migration + org-scoping
// assertions against real PostgreSQL. It skips unless TEST_DATABASE_URL is
// set (CI sets it); local runs use SQLite.
func TestPostgresMigrateAndScope(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping Postgres test")
	}

	s, err := Open(Options{Driver: "postgres", DSN: dsn, AutoMigrate: true})
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	// Migrate twice: idempotency must hold on Postgres too.
	require.NoError(t, s.Migrate())
	require.NoError(t, s.SeedDefaults(context.Background()))

	ctx := context.Background()
	o := Org{Name: "pg-org", Slug: "pg-org-scope-test"}
	require.NoError(t, s.Unscoped().WithContext(ctx).Create(&o).Error)

	n := Node{TenantBase: TenantBase{OrgID: o.ID}, Name: "pg-node"}
	require.NoError(t, s.Unscoped().Create(&n).Error)

	q, err := s.Org(orgCtx(o.ID))
	require.NoError(t, err)
	var nodes []Node
	require.NoError(t, q.Find(&nodes).Error)
	require.NotEmpty(t, nodes)
	for _, got := range nodes {
		require.Equal(t, o.ID, got.OrgID)
	}

	_, err = s.Org(context.Background())
	require.ErrorIs(t, err, ErrNoOrg)
}
