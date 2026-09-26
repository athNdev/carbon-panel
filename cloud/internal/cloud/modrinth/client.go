package modrinth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
)

const (
	defaultBaseURL   = "https://api.modrinth.com/v2"
	defaultUserAgent = "carbon-panel/1.0 (https://github.com/athNdev/carbon-panel)"
)

// Client interacts with the public Modrinth API v2.
type Client struct {
	baseURL    string
	userAgent  string
	httpClient *http.Client
}

// NewClient returns a new Modrinth client with reasonable timeouts.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 15 * time.Second,
		}
	}
	return &Client{
		baseURL:    defaultBaseURL,
		userAgent:  defaultUserAgent,
		httpClient: httpClient,
	}
}

// SetBaseURL overrides the base API URL (useful for testing).
func (c *Client) SetBaseURL(url string) {
	c.baseURL = strings.TrimRight(url, "/")
}

type modrinthSearchResponse struct {
	Hits      []modrinthHit `json:"hits"`
	Offset    int32         `json:"offset"`
	Limit     int32         `json:"limit"`
	TotalHits int32         `json:"total_hits"`
}

type modrinthHit struct {
	ProjectID     string   `json:"project_id"`
	ProjectType   string   `json:"project_type"`
	Slug          string   `json:"slug"`
	Author        string   `json:"author"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Categories    []string `json:"categories"`
	Versions      []string `json:"versions"`
	Downloads     int64    `json:"downloads"`
	Follows       int64    `json:"follows"`
	IconURL       string   `json:"icon_url"`
	LatestVersion string   `json:"latest_version"`
	ClientSide    string   `json:"client_side"`
	ServerSide    string   `json:"server_side"`
}

type modrinthProject struct {
	ID           string   `json:"id"`
	Slug         string   `json:"slug"`
	ProjectType  string   `json:"project_type"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Categories   []string `json:"categories"`
	Loaders      []string `json:"loaders"`
	GameVersions []string `json:"game_versions"`
	Downloads    int64    `json:"downloads"`
	Follows      int64    `json:"follows"`
	IconURL      string   `json:"icon_url"`
	ClientSide   string   `json:"client_side"`
	ServerSide   string   `json:"server_side"`
}

type modrinthVersion struct {
	ID            string                `json:"id"`
	ProjectID     string                `json:"project_id"`
	Name          string                `json:"name"`
	VersionNumber string                `json:"version_number"`
	GameVersions  []string              `json:"game_versions"`
	Loaders       []string              `json:"loaders"`
	DatePublished string                `json:"date_published"`
	Files         []modrinthVersionFile `json:"files"`
}

type modrinthVersionFile struct {
	Hashes   map[string]string `json:"hashes"`
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Primary  bool              `json:"primary"`
	Size     int64             `json:"size"`
}

// Search queries Modrinth v2 search endpoint.
func (c *Client) Search(ctx context.Context, req *v1.SearchAddonsRequest) (*v1.SearchAddonsResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	u, err := url.Parse(c.baseURL + "/search")
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("query", req.Query)
	q.Set("limit", strconv.Itoa(int(limit)))
	q.Set("offset", strconv.Itoa(int(req.Offset)))

	// Build facets
	var facetGroups [][]string
	switch req.AddonType {
	case v1.AddonType_ADDON_TYPE_PLUGIN:
		facetGroups = append(facetGroups, []string{"project_type:plugin", "project_type:mod"})
	case v1.AddonType_ADDON_TYPE_MOD:
		facetGroups = append(facetGroups, []string{"project_type:mod"})
	}

	if req.Loader != "" {
		facetGroups = append(facetGroups, []string{"categories:" + strings.ToLower(req.Loader)})
	}
	if req.GameVersion != "" {
		facetGroups = append(facetGroups, []string{"versions:" + req.GameVersion})
	}

	if len(facetGroups) > 0 {
		facetBytes, _ := json.Marshal(facetGroups)
		q.Set("facets", string(facetBytes))
	}

	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("modrinth search error (status %d): %s", resp.StatusCode, string(body))
	}

	var raw modrinthSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode modrinth search response: %w", err)
	}

	res := &v1.SearchAddonsResponse{
		Offset:    raw.Offset,
		Limit:     raw.Limit,
		TotalHits: raw.TotalHits,
	}

	for _, hit := range raw.Hits {
		aType := v1.AddonType_ADDON_TYPE_MOD
		if hit.ProjectType == "plugin" {
			aType = v1.AddonType_ADDON_TYPE_PLUGIN
		}
		res.Hits = append(res.Hits, &v1.AddonSearchResult{
			ProjectId:     hit.ProjectID,
			Slug:          hit.Slug,
			Title:         hit.Title,
			Description:   hit.Description,
			Categories:    hit.Categories,
			ClientSide:    hit.ClientSide,
			ServerSide:    hit.ServerSide,
			IconUrl:       hit.IconURL,
			Downloads:     hit.Downloads,
			Follows:       hit.Follows,
			GameVersions:  hit.Versions,
			LatestVersion: hit.LatestVersion,
			AddonType:     aType,
			Author:        hit.Author,
		})
	}

	return res, nil
}

// GetProject fetches project metadata from Modrinth.
func (c *Client) GetProject(ctx context.Context, idOrSlug string) (*v1.AddonSearchResult, error) {
	if strings.TrimSpace(idOrSlug) == "" {
		return nil, errors.New("project id or slug is required")
	}

	endpoint := fmt.Sprintf("%s/project/%s", c.baseURL, url.PathEscape(idOrSlug))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("modrinth project error (status %d): %s", resp.StatusCode, string(body))
	}

	var p modrinthProject
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("decode modrinth project: %w", err)
	}

	aType := v1.AddonType_ADDON_TYPE_MOD
	if p.ProjectType == "plugin" {
		aType = v1.AddonType_ADDON_TYPE_PLUGIN
	}

	return &v1.AddonSearchResult{
		ProjectId:    p.ID,
		Slug:         p.Slug,
		Title:        p.Title,
		Description:  p.Description,
		Categories:   p.Categories,
		ClientSide:   p.ClientSide,
		ServerSide:   p.ServerSide,
		IconUrl:      p.IconURL,
		Downloads:    p.Downloads,
		Follows:      p.Follows,
		Loaders:      p.Loaders,
		GameVersions: p.GameVersions,
		AddonType:    aType,
	}, nil
}

// GetVersions fetches versions for a project with optional loader and game_version filters.
func (c *Client) GetVersions(ctx context.Context, idOrSlug, loader, gameVersion string) ([]*v1.AddonVersion, error) {
	if strings.TrimSpace(idOrSlug) == "" {
		return nil, errors.New("project id or slug is required")
	}

	u, err := url.Parse(fmt.Sprintf("%s/project/%s/version", c.baseURL, url.PathEscape(idOrSlug)))
	if err != nil {
		return nil, err
	}

	q := u.Query()
	if loader != "" {
		loadersJson, _ := json.Marshal([]string{strings.ToLower(loader)})
		q.Set("loaders", string(loadersJson))
	}
	if gameVersion != "" {
		versionsJson, _ := json.Marshal([]string{gameVersion})
		q.Set("game_versions", string(versionsJson))
	}
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("modrinth versions error (status %d): %s", resp.StatusCode, string(body))
	}

	var rawVersions []modrinthVersion
	if err := json.NewDecoder(resp.Body).Decode(&rawVersions); err != nil {
		return nil, fmt.Errorf("decode modrinth versions: %w", err)
	}

	var results []*v1.AddonVersion
	for _, v := range rawVersions {
		var files []*v1.AddonVersionFile
		for _, f := range v.Files {
			sha512 := ""
			if f.Hashes != nil {
				sha512 = f.Hashes["sha512"]
			}
			files = append(files, &v1.AddonVersionFile{
				Url:      f.URL,
				Filename: f.Filename,
				Primary:  f.Primary,
				Size:     f.Size,
				Sha512:   sha512,
			})
		}

		results = append(results, &v1.AddonVersion{
			Id:            v.ID,
			VersionNumber: v.VersionNumber,
			Name:          v.Name,
			GameVersions:  v.GameVersions,
			Loaders:       v.Loaders,
			Files:         files,
			DatePublished: v.DatePublished,
		})
	}

	return results, nil
}

// ResolveDownloadURL resolves the direct download URL and filename from project/version parameters.
func (c *Client) ResolveDownloadURL(ctx context.Context, projectIDOrSlug, versionID, loader, gameVersion string) (string, string, error) {
	if versionID != "" {
		endpoint := fmt.Sprintf("%s/version/%s", c.baseURL, url.PathEscape(versionID))
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return "", "", err
		}
		httpReq.Header.Set("User-Agent", c.userAgent)

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			return "", "", err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return "", "", fmt.Errorf("modrinth get version error (status %d): %s", resp.StatusCode, string(body))
		}

		var v modrinthVersion
		if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
			return "", "", fmt.Errorf("decode modrinth version: %w", err)
		}

		for _, f := range v.Files {
			if f.Primary {
				return f.URL, f.Filename, nil
			}
		}
		if len(v.Files) > 0 {
			return v.Files[0].URL, v.Files[0].Filename, nil
		}
		return "", "", errors.New("no files found in version")
	}

	// Find latest compatible version
	versions, err := c.GetVersions(ctx, projectIDOrSlug, loader, gameVersion)
	if err != nil {
		return "", "", err
	}
	if len(versions) == 0 {
		return "", "", fmt.Errorf("no compatible versions found for project %s (loader: %s, version: %s)", projectIDOrSlug, loader, gameVersion)
	}

	// Select first version (latest) and its primary file
	latest := versions[0]
	for _, f := range latest.Files {
		if f.Primary {
			return f.Url, f.Filename, nil
		}
	}
	if len(latest.Files) > 0 {
		return latest.Files[0].Url, latest.Files[0].Filename, nil
	}

	return "", "", errors.New("no files found in latest compatible version")
}
