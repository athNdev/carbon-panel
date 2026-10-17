package db

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// OpenTest opens an isolated SQLite store with migrations applied. Every test
// uses it; each caller gets its own temp-file database.
func OpenTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(Options{
		Driver:      "sqlite",
		DSN:         filepath.Join(t.TempDir(), "test.db"),
		AutoMigrate: true,
	})
	require.NoError(t, err)
	require.NoError(t, s.SeedDefaults(t.Context()))
	return s
}

func TestOpenSQLiteMemory(t *testing.T) {
	t.Parallel()
	s, err := Open(Options{Driver: "sqlite", DSN: "file::memory:?cache=shared"})
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
}

func TestOpenUnknownDriver(t *testing.T) {
	t.Parallel()
	_, err := Open(Options{Driver: "cockroach", DSN: "nonsense"})
	require.Error(t, err)
}

func TestMigrateIdempotent(t *testing.T) {
	t.Parallel()
	s := OpenTest(t)
	require.NoError(t, s.Migrate())
	require.NoError(t, s.Migrate())

	var count int64
	require.NoError(t, s.Unscoped().Model(&Org{}).Count(&count).Error)
	require.NoError(t, s.SeedDefaults(t.Context()))
	require.NoError(t, s.Migrate())
}

func TestUUIDPrimaryKeysGenerated(t *testing.T) {
	t.Parallel()
	s := OpenTest(t)
	org := Org{Name: "Acme", Slug: "acme"}
	require.NoError(t, s.Unscoped().Create(&org).Error)
	require.NotEmpty(t, org.ID)

	m := Member{TenantBase: TenantBase{OrgID: org.ID}, UserID: "u1"}
	require.NoError(t, s.Unscoped().Create(&m).Error)
	require.NotEmpty(t, m.ID)

	ev := AuditEvent{OrgID: org.ID, Action: "test.ping"}
	require.NoError(t, s.Unscoped().Create(&ev).Error)
	require.NotEmpty(t, ev.ID)
}

func TestJSONAccessors(t *testing.T) {
	t.Parallel()

	n := &Node{}
	n.SetCapacity(NodeCapacity{VCPU: 4, RAMMB: 8192, DiskGB: 80})
	require.Equal(t, NodeCapacity{VCPU: 4, RAMMB: 8192, DiskGB: 80}, n.CapacityDecoded())
	n.SetLabels(map[string]string{"env": "prod"})
	require.Equal(t, map[string]string{"env": "prod"}, n.LabelMap())

	k := &ApiKey{}
	k.SetPermissions([]string{"nodes:read", "nodes:write"})
	require.Equal(t, []string{"nodes:read", "nodes:write"}, k.PermissionList())

	rb := &RoleBinding{}
	rb.SetPermissions([]string{"workloads:run"})
	require.Equal(t, []string{"workloads:run"}, rb.PermissionList())

	nt := &NodeType{}
	nt.SetInstanceTypes(map[string]string{"hetzner": "cx42"})
	require.Equal(t, map[string]string{"hetzner": "cx42"}, nt.InstanceTypeMap())

	p := &Provision{}
	p.SetOutputs(map[string]string{"ip": "10.0.0.1"})
	require.Equal(t, map[string]string{"ip": "10.0.0.1"}, p.OutputMap())

	w := &Workload{}
	w.SetSpec(map[string]any{"image": "paper"})
	require.Equal(t, "paper", w.SpecMap()["image"])

	// Empty raw JSON decodes to zero values, never errors.
	require.Equal(t, NodeCapacity{}, (&Node{}).CapacityDecoded())
	require.Empty(t, (&ApiKey{}).PermissionList())
}
