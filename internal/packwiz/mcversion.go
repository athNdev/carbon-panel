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
// mod versions, and reports every mod that is now stale. Direct-URL ("url")
// mods carry no MC-scoped upstream version, so they are never marked stale.
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
		if mod.Platform == "url" {
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
		if mod.Platform == "url" {
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
