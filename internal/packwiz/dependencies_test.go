package packwiz

import (
	"fmt"
	"os"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubFetcher is an in-memory VersionDependencyFetcher: graph maps
// "platform:projectID" -> required dependency project IDs, meta provides the
// install metadata returned for each node. Missing meta entries error.
type stubFetcher struct {
	graph map[string][]string
	meta  map[string]ModItem
}

func (s *stubFetcher) LatestCompatibleMod(platform, projectID, _, _ string) (ModItem, []DependencyRef, error) {
	key := depKey(platform, projectID)
	mod, ok := s.meta[key]
	if !ok {
		return ModItem{}, nil, fmt.Errorf("unknown project %s", key)
	}
	var deps []DependencyRef
	for _, depID := range s.graph[key] {
		deps = append(deps, DependencyRef{Platform: "modrinth", ProjectID: depID, DependencyType: "required"})
	}
	return mod, deps, nil
}

func stubMod(projectID, name string) ModItem {
	return ModItem{
		Slug:        projectID,
		Name:        name,
		FileName:    projectID + ".jar",
		Side:        "both",
		Platform:    "modrinth",
		ProjectID:   projectID,
		VersionID:   "v1",
		DownloadURL: "https://example.com/" + projectID + ".jar",
	}
}

func newDepTestManager(t *testing.T) *Manager {
	t.Helper()
	dir, err := os.MkdirTemp("", "packwiz_deps_test_*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	mgr := NewManager(dir, logger.New())
	_, err = mgr.CreatePack(&Pack{ID: "p1", Name: "P", MCVersion: "1.20.1", ModLoader: "fabric"})
	require.NoError(t, err)
	return mgr
}

func TestResolveTransitiveDependencies_Chain(t *testing.T) {
	mgr := newDepTestManager(t)
	f := &stubFetcher{
		graph: map[string][]string{
			"modrinth:a": {"b"},
			"modrinth:b": {"c"},
		},
		meta: map[string]ModItem{
			"modrinth:a": stubMod("a", "A"),
			"modrinth:b": stubMod("b", "B"),
			"modrinth:c": stubMod("c", "C"),
		},
	}

	deps, err := mgr.ResolveTransitiveDependencies(f, "modrinth", "a", "fabric", "1.20.1", 0)
	require.NoError(t, err)
	assert.Len(t, deps, 2)
	assert.Equal(t, "b", deps[0].ProjectID)
	assert.Equal(t, "c", deps[1].ProjectID)
}

func TestResolveTransitiveDependencies_CycleTerminates(t *testing.T) {
	mgr := newDepTestManager(t)
	f := &stubFetcher{
		graph: map[string][]string{
			"modrinth:a": {"b"},
			"modrinth:b": {"a", "c"},
		},
		meta: map[string]ModItem{
			"modrinth:a": stubMod("a", "A"),
			"modrinth:b": stubMod("b", "B"),
			"modrinth:c": stubMod("c", "C"),
		},
	}

	deps, err := mgr.ResolveTransitiveDependencies(f, "modrinth", "a", "fabric", "1.20.1", 0)
	require.NoError(t, err)
	// b and c only — the root "a" must never reappear via the cycle.
	assert.Len(t, deps, 2)
}

func TestResolveTransitiveDependencies_DepthBound(t *testing.T) {
	mgr := newDepTestManager(t)
	f := &stubFetcher{
		graph: map[string][]string{
			"modrinth:a": {"b"},
			"modrinth:b": {"c"},
			"modrinth:c": {"d"},
		},
		meta: map[string]ModItem{
			"modrinth:a": stubMod("a", "A"),
			"modrinth:b": stubMod("b", "B"),
			"modrinth:c": stubMod("c", "C"),
			"modrinth:d": stubMod("d", "D"),
		},
	}

	deps, err := mgr.ResolveTransitiveDependencies(f, "modrinth", "a", "fabric", "1.20.1", 2)
	require.NoError(t, err)
	assert.Len(t, deps, 2) // b, c — d is beyond maxDepth
}

func TestResolveTransitiveDependencies_NilFetcher(t *testing.T) {
	mgr := newDepTestManager(t)
	_, err := mgr.ResolveTransitiveDependencies(nil, "modrinth", "a", "fabric", "1.20.1", 0)
	assert.Error(t, err)
}

func TestAddModWithDependencies_InstallsSkipsUnresolved(t *testing.T) {
	mgr := newDepTestManager(t)

	// "present" is already in the pack: it must be skipped, not reinstalled.
	require.NoError(t, mgr.AddMod("p1", stubMod("present", "Present")))

	f := &stubFetcher{
		graph: map[string][]string{
			"modrinth:root": {"dep-new", "present", "dep-missing"},
		},
		meta: map[string]ModItem{
			"modrinth:root":    stubMod("root", "Root"),
			"modrinth:dep-new": stubMod("dep-new", "Dep New"),
			"modrinth:present": stubMod("present", "Present"),
			// dep-missing has no meta -> unresolvable
		},
	}

	report, err := mgr.AddModWithDependencies("p1", stubMod("root", "Root"), "fabric", "1.20.1", f)
	require.NoError(t, err)
	assert.Len(t, report.Added, 1)
	assert.Equal(t, "dep-new", report.Added[0].ProjectID)
	assert.Contains(t, report.Skipped, "present")
	assert.Len(t, report.Unresolved, 1)
	assert.Equal(t, "dep-missing", report.Unresolved[0].ProjectID)

	pack, err := mgr.GetPack("p1")
	require.NoError(t, err)
	assert.Len(t, pack.Mods, 3) // present + root + dep-new
}
