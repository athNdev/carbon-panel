package packwiz

import (
	"fmt"
	"time"
)

// StaleModInfo marks a single mod as stale after the pack's Minecraft version
// changed: its installed version was resolved for the previous MC version and
// must be re-checked (update) before the pack is consistent again.
type StaleModInfo struct {
	Slug             string `json:"slug"`
	Name             string `json:"name"`
	Platform         string `json:"platform"`
	CurrentVersionID string `json:"current_version_id"`
	Reason           string `json:"reason"` // "mc_version_changed"
}

// StaleReport is returned when a pack's Minecraft version changes.
type StaleReport struct {
	PackID      string        `json:"pack_id"`
	PreviousMC  string        `json:"previous_mc"`
	NewMC       string        `json:"new_mc"`
	StaleMods   []StaleModInfo `json:"stale_mods"`
	StaleCount  int           `json:"stale_count"`
}

// SetMCVersion switches the pack to newMCVersion without touching installed
// mod versions, and reports every mod that is now stale. Only version-tracked
// mods (curseforge, or modrinth with a project ID) carry an MC-scoped upstream
// version: direct-URL ("url") mods — including ones that round-tripped through
// pack files without update metadata — are never marked stale.
// Callers should follow up with SimulateMigration/ApplyMigration to offer the
// update-and-resolve-conflicts flow.
func (m *Manager) SetMCVersion(packID, newMCVersion string) (*StaleReport, error) {
	if newMCVersion == "" {
		return nil, fmt.Errorf("target MC version is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	pack, err := m.readPack(packID)
	if err != nil {
		return nil, err
	}

	report := &StaleReport{
		PackID:     packID,
		PreviousMC: pack.MCVersion,
		NewMC:      newMCVersion,
		StaleMods:  []StaleModInfo{},
	}

	if pack.MCVersion == newMCVersion {
		return report, nil
	}

	pack.MCVersion = newMCVersion
	pack.UpdatedAt = time.Now()

	for _, mod := range pack.Mods {
		if !isVersionTracked(mod) {
			continue
		}
		report.StaleMods = append(report.StaleMods, StaleModInfo{
			Slug:             mod.Slug,
			Name:             mod.Name,
			Platform:         mod.Platform,
			CurrentVersionID: mod.VersionID,
			Reason:           "mc_version_changed",
		})
	}
	report.StaleCount = len(report.StaleMods)

	if err := m.writePackFiles(pack); err != nil {
		return nil, err
	}
	return report, nil
}

// isVersionTracked reports whether a mod has upstream version metadata that
// is scoped to a Minecraft version. Pack files without an update block
// round-trip as platform "modrinth" with an empty project ID; those carry no
// resolvable upstream version and are treated like direct-URL mods.
func isVersionTracked(mod ModItem) bool {
	switch mod.Platform {
	case "curseforge":
		return true
	case "modrinth":
		return mod.ProjectID != ""
	default:
		return false
	}
}

// DryRunMCVersion reports which mods would go stale if the pack moved to
// newMCVersion, without writing anything.
func (m *Manager) DryRunMCVersion(packID, newMCVersion string) (*StaleReport, error) {
	if newMCVersion == "" {
		return nil, fmt.Errorf("target MC version is required")
	}

	pack, err := m.GetPack(packID)
	if err != nil {
		return nil, err
	}

	report := &StaleReport{
		PackID:     packID,
		PreviousMC: pack.MCVersion,
		NewMC:      newMCVersion,
		StaleMods:  []StaleModInfo{},
	}
	if pack.MCVersion == newMCVersion {
		return report, nil
	}
	for _, mod := range pack.Mods {
		if !isVersionTracked(mod) {
			continue
		}
		report.StaleMods = append(report.StaleMods, StaleModInfo{
			Slug:             mod.Slug,
			Name:             mod.Name,
			Platform:         mod.Platform,
			CurrentVersionID: mod.VersionID,
			Reason:           "mc_version_changed",
		})
	}
	report.StaleCount = len(report.StaleMods)
	return report, nil
}
