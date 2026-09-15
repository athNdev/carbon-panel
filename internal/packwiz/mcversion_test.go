package packwiz

import (
	"os"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMCVersionTestManager(t *testing.T) *Manager {
	t.Helper()
	dir, err := os.MkdirTemp("", "packwiz_mcversion_test_*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	mgr := NewManager(dir, logger.New())
	_, err = mgr.CreatePack(&Pack{ID: "p1", Name: "P", MCVersion: "1.20.1", ModLoader: "fabric"})
	require.NoError(t, err)
	require.NoError(t, mgr.AddMod("p1", ModItem{
		Slug: "sodium", Name: "Sodium", FileName: "sodium.jar", Side: "both",
		Platform: "modrinth", ProjectID: "AANobbMI", VersionID: "v-old",
		DownloadURL: "https://example.com/sodium.jar",
	}))
	require.NoError(t, mgr.AddMod("p1", ModItem{
		Slug: "custom", Name: "Custom", FileName: "custom.jar", Side: "both",
		Platform: "url", ProjectID: "custom", VersionID: "abc",
		DownloadURL: "https://example.com/custom.jar",
	}))
	return mgr
}

func TestSetMCVersion_MarksModsStale(t *testing.T) {
	mgr := newMCVersionTestManager(t)

	report, err := mgr.SetMCVersion("p1", "1.21.1")
	require.NoError(t, err)
	assert.Equal(t, "1.20.1", report.PreviousMC)
	assert.Equal(t, "1.21.1", report.NewMC)
	// Only the modrinth mod is stale; the direct-URL mod is excluded.
	require.Len(t, report.StaleMods, 1)
	assert.Equal(t, "sodium", report.StaleMods[0].Slug)
	assert.Equal(t, "v-old", report.StaleMods[0].CurrentVersionID)
	assert.Equal(t, "mc_version_changed", report.StaleMods[0].Reason)
	assert.Equal(t, 1, report.StaleCount)

	// The pack version moved but installed mod versions are untouched.
	pack, err := mgr.GetPack("p1")
	require.NoError(t, err)
	assert.Equal(t, "1.21.1", pack.MCVersion)
	for _, mod := range pack.Mods {
		if mod.Slug == "sodium" {
			assert.Equal(t, "v-old", mod.VersionID)
		}
	}
}

func TestSetMCVersion_SameVersionNoop(t *testing.T) {
	mgr := newMCVersionTestManager(t)
	report, err := mgr.SetMCVersion("p1", "1.20.1")
	require.NoError(t, err)
	assert.Empty(t, report.StaleMods)
}

func TestSetMCVersion_RequiresTarget(t *testing.T) {
	mgr := newMCVersionTestManager(t)
	_, err := mgr.SetMCVersion("p1", "")
	assert.Error(t, err)
	_, err = mgr.SetMCVersion("nonexistent", "1.21.1")
	assert.Error(t, err)
}

func TestDryRunMCVersion_NoWrite(t *testing.T) {
	mgr := newMCVersionTestManager(t)

	report, err := mgr.DryRunMCVersion("p1", "1.21.1")
	require.NoError(t, err)
	assert.Equal(t, 1, report.StaleCount)

	pack, err := mgr.GetPack("p1")
	require.NoError(t, err)
	assert.Equal(t, "1.20.1", pack.MCVersion, "dry run must not persist the version change")
}
