package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type loaderCacheEntry struct {
	versions  []string
	expiresAt time.Time
}

var (
	loaderCacheMu sync.RWMutex
	loaderCache   = make(map[string]loaderCacheEntry)
	loaderHTTP    = &http.Client{Timeout: 5 * time.Second}
)

var defaultLoaderVersions = map[string][]string{
	"fabric":   {"latest", "0.16.10", "0.16.9", "0.16.7", "0.16.5", "0.15.11", "0.15.7", "0.14.25"},
	"quilt":    {"latest", "0.26.1", "0.25.0", "0.24.1", "0.23.1", "0.22.0"},
	"forge":    {"latest", "54.0.1", "52.0.1", "49.0.38", "47.3.0", "47.2.0", "43.3.0", "40.2.14", "36.2.39", "14.23.5.2860"},
	"neoforge": {"latest", "21.1.84", "21.1.72", "21.0.167", "20.6.119", "20.4.237", "20.2.86", "20.1.0"},
}

// GetLoaderVersions fetches valid loader versions for a given mod loader and Minecraft game version.
func GetLoaderVersions(ctx context.Context, loader, gameVersion string) []string {
	loader = strings.ToLower(strings.TrimSpace(loader))
	gameVersion = strings.TrimSpace(gameVersion)

	cacheKey := fmt.Sprintf("%s:%s", loader, gameVersion)
	loaderCacheMu.RLock()
	if entry, ok := loaderCache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		loaderCacheMu.RUnlock()
		return entry.versions
	}
	loaderCacheMu.RUnlock()

	var versions []string
	switch loader {
	case "fabric":
		versions = fetchFabricLoaderVersions(ctx, gameVersion)
	case "quilt":
		versions = fetchQuiltLoaderVersions(ctx, gameVersion)
	case "forge":
		versions = fetchForgeLoaderVersions(ctx, gameVersion)
	case "neoforge":
		versions = fetchNeoForgeLoaderVersions(ctx, gameVersion)
	default:
		versions = []string{"latest"}
	}

	if len(versions) == 0 {
		if def, ok := defaultLoaderVersions[loader]; ok {
			versions = def
		} else {
			versions = []string{"latest"}
		}
	}

	// Guarantee "latest" is present at the front
	hasLatest := false
	for _, v := range versions {
		if strings.EqualFold(v, "latest") {
			hasLatest = true
			break
		}
	}
	if !hasLatest {
		versions = append([]string{"latest"}, versions...)
	}

	loaderCacheMu.Lock()
	loaderCache[cacheKey] = loaderCacheEntry{
		versions:  versions,
		expiresAt: time.Now().Add(1 * time.Hour),
	}
	loaderCacheMu.Unlock()

	return versions
}

func fetchFabricLoaderVersions(ctx context.Context, gameVersion string) []string {
	reqURL := "https://meta.fabricmc.net/v2/versions/loader"
	if gameVersion != "" {
		reqURL = fmt.Sprintf("https://meta.fabricmc.net/v2/versions/loader/%s", gameVersion)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "CarbonPanel/1.0 (CarbonPanel)")

	resp, err := loaderHTTP.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()

	if gameVersion != "" {
		var list []struct {
			Loader struct {
				Version string `json:"version"`
				Stable  bool   `json:"stable"`
			} `json:"loader"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			return nil
		}
		var results []string
		seen := make(map[string]bool)
		for _, item := range list {
			v := strings.TrimSpace(item.Loader.Version)
			if v != "" && !seen[v] {
				seen[v] = true
				results = append(results, v)
				if len(results) >= 20 {
					break
				}
			}
		}
		return results
	}

	var list []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil
	}
	var results []string
	for _, item := range list {
		if item.Version != "" {
			results = append(results, item.Version)
			if len(results) >= 20 {
				break
			}
		}
	}
	return results
}

func fetchQuiltLoaderVersions(ctx context.Context, gameVersion string) []string {
	reqURL := "https://meta.quiltmc.net/v3/versions/loader"
	if gameVersion != "" {
		reqURL = fmt.Sprintf("https://meta.quiltmc.net/v3/versions/loader/%s", gameVersion)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "CarbonPanel/1.0 (CarbonPanel)")

	resp, err := loaderHTTP.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()

	var list []struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil
	}
	var results []string
	for _, item := range list {
		if item.Version != "" {
			results = append(results, item.Version)
			if len(results) >= 15 {
				break
			}
		}
	}
	return results
}

func fetchForgeLoaderVersions(ctx context.Context, gameVersion string) []string {
	reqURL := "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "CarbonPanel/1.0 (CarbonPanel)")

	resp, err := loaderHTTP.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()

	var data struct {
		Promos map[string]string `json:"promos"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}

	var results []string
	seen := make(map[string]bool)

	if gameVersion != "" {
		recKey := fmt.Sprintf("%s-recommended", gameVersion)
		latestKey := fmt.Sprintf("%s-latest", gameVersion)

		if v, ok := data.Promos[latestKey]; ok && v != "" && !seen[v] {
			seen[v] = true
			results = append(results, v)
		}
		if v, ok := data.Promos[recKey]; ok && v != "" && !seen[v] {
			seen[v] = true
			results = append(results, v)
		}
	}

	for k, v := range data.Promos {
		if strings.HasSuffix(k, "-latest") || strings.HasSuffix(k, "-recommended") {
			if gameVersion == "" || strings.HasPrefix(k, gameVersion) {
				if !seen[v] && v != "" {
					seen[v] = true
					results = append(results, v)
					if len(results) >= 15 {
						break
					}
				}
			}
		}
	}

	return results
}

func fetchNeoForgeLoaderVersions(ctx context.Context, gameVersion string) []string {
	reqURL := "https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/neoforge"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "CarbonPanel/1.0 (CarbonPanel)")

	resp, err := loaderHTTP.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()

	var data struct {
		Versions []string `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}

	targetPrefix := ""
	if gameVersion != "" {
		parts := strings.Split(gameVersion, ".")
		if len(parts) >= 2 && parts[0] == "1" {
			major := parts[1]
			minor := "0"
			if len(parts) >= 3 {
				minor = parts[2]
			}
			targetPrefix = fmt.Sprintf("%s.%s.", major, minor)
		}
	}

	var results []string
	for i := len(data.Versions) - 1; i >= 0; i-- {
		v := data.Versions[i]
		if targetPrefix == "" || strings.HasPrefix(v, targetPrefix) {
			results = append(results, v)
			if len(results) >= 20 {
				break
			}
		}
	}

	return results
}
