package packwiz

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// DependencyRef describes a single mod dependency discovered during resolution.
type DependencyRef struct {
	Platform       string `json:"platform"`        // "modrinth" | "curseforge"
	ProjectID      string `json:"project_id"`      // Modrinth project id/slug or CurseForge numeric mod id
	VersionID      string `json:"version_id,omitempty"`
	Title          string `json:"title,omitempty"`
	DependencyType string `json:"dependency_type"` // "required" | "optional"
}

// DependencyInstallReport summarises a dependency-aware mod install.
type DependencyInstallReport struct {
	Root       ModItem         `json:"root"`
	Added      []ModItem       `json:"added"`      // transitive deps newly installed
	Skipped    []string        `json:"skipped"`    // slugs already present in the pack
	Unresolved []DependencyRef `json:"unresolved"` // required deps with no compatible version
}

// VersionDependencyFetcher resolves the latest compatible version metadata for
// a project plus that version's own required dependencies. The default
// implementation queries the Modrinth / CurseForge APIs over HTTP; tests inject
// a stub.
type VersionDependencyFetcher interface {
	// LatestCompatibleMod returns install metadata for the newest version of
	// projectID compatible with loader/mcVersion, plus its required deps.
	LatestCompatibleMod(platform, projectID, loader, mcVersion string) (ModItem, []DependencyRef, error)
}

// MaxDependencyDepth bounds transitive resolution to avoid runaway API fan-out.
const MaxDependencyDepth = 5

// depKey uniquely identifies a project across platforms for cycle detection.
func depKey(platform, projectID string) string {
	return strings.ToLower(platform) + ":" + strings.ToLower(projectID)
}

// ResolveTransitiveDependencies walks required dependencies breadth-first,
// cycle-safe and depth-bounded. It returns every distinct required dependency
// (excluding the root project itself).
func (m *Manager) ResolveTransitiveDependencies(fetcher VersionDependencyFetcher, platform, projectID, loader, mcVersion string, maxDepth int) ([]DependencyRef, error) {
	if fetcher == nil {
		return nil, fmt.Errorf("dependency fetcher is required")
	}
	if maxDepth <= 0 {
		maxDepth = MaxDependencyDepth
	}

	visited := map[string]bool{depKey(platform, projectID): true}
	var resolved []DependencyRef

	type queueItem struct {
		platform  string
		projectID string
		depth     int
	}
	queue := []queueItem{{platform: platform, projectID: projectID, depth: 0}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= maxDepth {
			continue
		}

		_, deps, err := fetcher.LatestCompatibleMod(cur.platform, cur.projectID, loader, mcVersion)
		if err != nil {
			// A single unresolvable node must not abort the whole walk;
			// the caller records it via AddModWithDependencies instead.
			continue
		}
		for _, d := range deps {
			if !strings.EqualFold(d.DependencyType, "required") {
				continue
			}
			key := depKey(d.Platform, d.ProjectID)
			if d.ProjectID == "" || visited[key] {
				continue
			}
			visited[key] = true
			resolved = append(resolved, d)
			queue = append(queue, queueItem{platform: d.Platform, projectID: d.ProjectID, depth: cur.depth + 1})
		}
	}

	if resolved == nil {
		resolved = []DependencyRef{}
	}
	return resolved, nil
}

// AddModWithDependencies installs mod plus all of its transitive required
// dependencies (for loader/mcVersion), skipping anything already in the pack.
// Unresolvable required deps are reported, never fatal.
func (m *Manager) AddModWithDependencies(packID string, mod ModItem, loader, mcVersion string, fetcher VersionDependencyFetcher) (*DependencyInstallReport, error) {
	if fetcher == nil {
		return nil, fmt.Errorf("dependency fetcher is required")
	}

	if err := m.AddMod(packID, mod); err != nil {
		return nil, err
	}

	report := &DependencyInstallReport{Root: mod, Added: []ModItem{}, Skipped: []string{}, Unresolved: []DependencyRef{}}

	pack, err := m.GetPack(packID)
	if err != nil {
		return nil, err
	}
	presentByProject := map[string]bool{}
	presentBySlug := map[string]bool{}
	for _, item := range pack.Mods {
		if item.ProjectID != "" {
			presentByProject[depKey(item.Platform, item.ProjectID)] = true
		}
		if item.Slug != "" {
			presentBySlug[strings.ToLower(item.Slug)] = true
		}
	}

	// Resolve the full transitive closure first (cycle-safe; the root is
	// treated as visited so a dependency cycle can never reinstall it),
	// then install every missing node.
	closure, err := m.ResolveTransitiveDependencies(fetcher, mod.Platform, mod.ProjectID, loader, mcVersion, MaxDependencyDepth)
	if err != nil {
		return report, nil
	}

	for _, d := range closure {
		key := depKey(d.Platform, d.ProjectID)
		if presentByProject[key] {
			report.Skipped = append(report.Skipped, d.ProjectID)
			continue
		}
		compat, _, err := fetcher.LatestCompatibleMod(d.Platform, d.ProjectID, loader, mcVersion)
		if err != nil {
			report.Unresolved = append(report.Unresolved, d)
			continue
		}
		if compat.Slug != "" && presentBySlug[strings.ToLower(compat.Slug)] {
			report.Skipped = append(report.Skipped, compat.Slug)
			continue
		}
		if compat.Side == "" {
			compat.Side = "both"
		}
		if err := m.AddMod(packID, compat); err != nil {
			report.Unresolved = append(report.Unresolved, d)
			continue
		}
		report.Added = append(report.Added, compat)
		presentByProject[key] = true
		if compat.Slug != "" {
			presentBySlug[strings.ToLower(compat.Slug)] = true
		}
	}

	return report, nil
}

// httpDependencyFetcher is the production VersionDependencyFetcher backed by
// the Modrinth API and the keyless CurseForge community proxy (the same
// fallback chain used by the mod search handler).
type httpDependencyFetcher struct {
	manager *Manager
}

// NewHTTPDependencyFetcher returns the production fetcher for a Manager.
func (m *Manager) NewHTTPDependencyFetcher() VersionDependencyFetcher {
	return &httpDependencyFetcher{manager: m}
}

func (f *httpDependencyFetcher) LatestCompatibleMod(platform, projectID, loader, mcVersion string) (ModItem, []DependencyRef, error) {
	switch strings.ToLower(platform) {
	case "curseforge":
		return f.latestCurseForgeMod(projectID, loader, mcVersion)
	default:
		return f.latestModrinthMod(projectID, loader, mcVersion)
	}
}

func primaryFileName(files []struct {
	URL      string `json:"url"`
	FileName string `json:"filename"`
	Primary  bool   `json:"primary"`
	Size     int64  `json:"size"`
}) (url, name string, size int64) {
	if len(files) == 0 {
		return "", "", 0
	}
	sel := files[0]
	for _, f := range files {
		if f.Primary {
			sel = f
			break
		}
	}
	return sel.URL, sel.FileName, sel.Size
}

func (f *httpDependencyFetcher) latestModrinthMod(projectID, loader, mcVersion string) (ModItem, []DependencyRef, error) {
	reqURL := fmt.Sprintf("https://api.modrinth.com/v2/project/%s/version", url.PathEscape(projectID))
	params := url.Values{}
	if loader != "" && loader != "vanilla" {
		if b, err := json.Marshal([]string{strings.ToLower(loader)}); err == nil {
			params.Set("loaders", string(b))
		}
	}
	if mcVersion != "" {
		if b, err := json.Marshal([]string{mcVersion}); err == nil {
			params.Set("game_versions", string(b))
		}
	}
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	resp, err := f.manager.httpClient.Get(reqURL)
	if err != nil {
		return ModItem{}, nil, fmt.Errorf("modrinth request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ModItem{}, nil, fmt.Errorf("modrinth returned HTTP %d for %s", resp.StatusCode, projectID)
	}

	var versions []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		VersionNumber string `json:"version_number"`
		Files         []struct {
			URL      string `json:"url"`
			FileName string `json:"filename"`
			Primary  bool   `json:"primary"`
			Size     int64  `json:"size"`
		} `json:"files"`
		Dependencies []struct {
			ProjectID      *string `json:"project_id"`
			VersionID      *string `json:"version_id"`
			DependencyType string  `json:"dependency_type"`
		} `json:"dependencies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return ModItem{}, nil, fmt.Errorf("failed to parse modrinth versions: %w", err)
	}
	if len(versions) == 0 {
		return ModItem{}, nil, fmt.Errorf("no compatible version of %s for %s %s", projectID, loader, mcVersion)
	}

	latest := versions[0]
	dlURL, fileName, size := primaryFileName(latest.Files)
	title := latest.Name
	if title == "" {
		title = latest.VersionNumber
	}
	if title == "" {
		title = projectID
	}

	mod := ModItem{
		Slug:        sanitizeSlug(projectID),
		Name:        title,
		FileName:    fileName,
		Side:        "both",
		Platform:    "modrinth",
		ProjectID:   projectID,
		VersionID:   latest.ID,
		DownloadURL: dlURL,
		FileSize:    size,
	}

	var deps []DependencyRef
	for _, d := range latest.Dependencies {
		if d.ProjectID == nil || *d.ProjectID == "" {
			continue
		}
		vid := ""
		if d.VersionID != nil {
			vid = *d.VersionID
		}
		deps = append(deps, DependencyRef{
			Platform:       "modrinth",
			ProjectID:      *d.ProjectID,
			VersionID:      vid,
			DependencyType: d.DependencyType,
		})
	}
	return mod, deps, nil
}

func (f *httpDependencyFetcher) latestCurseForgeMod(modID, loader, mcVersion string) (ModItem, []DependencyRef, error) {
	reqURL := fmt.Sprintf("https://api.curse.tools/v1/cf/mods/%s/files?pageSize=50", url.PathEscape(modID))
	if mcVersion != "" {
		reqURL += "&gameVersion=" + url.QueryEscape(mcVersion)
	}

	resp, err := f.manager.httpClient.Get(reqURL)
	if err != nil {
		return ModItem{}, nil, fmt.Errorf("curseforge request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ModItem{}, nil, fmt.Errorf("curseforge returned HTTP %d for %s", resp.StatusCode, modID)
	}

	var payload struct {
		Data []struct {
			ID           int      `json:"id"`
			DisplayName  string   `json:"displayName"`
			FileName     string   `json:"fileName"`
			DownloadURL  string   `json:"downloadUrl"`
			FileLength   int64    `json:"fileLength"`
			GameVersions []string `json:"gameVersions"`
			Dependencies []struct {
				ModID        int `json:"modId"`
				RelationType int `json:"relationType"` // 3 == required
			} `json:"dependencies"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ModItem{}, nil, fmt.Errorf("failed to parse curseforge files: %w", err)
	}
	if len(payload.Data) == 0 {
		return ModItem{}, nil, fmt.Errorf("no compatible file of %s for %s", modID, mcVersion)
	}

	file := payload.Data[0]
	if loader != "" && loader != "vanilla" {
		for _, entry := range payload.Data {
			for _, gv := range entry.GameVersions {
				if strings.EqualFold(gv, loader) {
					file = entry
					break
				}
			}
		}
	}

	dlURL := file.DownloadURL
	if dlURL == "" && file.FileName != "" {
		dlURL = fmt.Sprintf("https://edge.forgecdn.net/files/%d/%d/%s", file.ID/1000, file.ID%1000, url.PathEscape(file.FileName))
	}

	mod := ModItem{
		Slug:        sanitizeSlug(fmt.Sprintf("cf-%s", modID)),
		Name:        file.DisplayName,
		FileName:    file.FileName,
		Side:        "both",
		Platform:    "curseforge",
		ProjectID:   modID,
		VersionID:   fmt.Sprintf("%d", file.ID),
		DownloadURL: dlURL,
		FileSize:    file.FileLength,
	}

	var deps []DependencyRef
	for _, d := range file.Dependencies {
		depType := "optional"
		if d.RelationType == 3 {
			depType = "required"
		}
		deps = append(deps, DependencyRef{
			Platform:       "curseforge",
			ProjectID:      fmt.Sprintf("%d", d.ModID),
			DependencyType: depType,
		})
	}
	return mod, deps, nil
}
