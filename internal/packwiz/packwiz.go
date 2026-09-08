package packwiz

import (
	"archive/zip"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/pkg/logger"
	toml "github.com/pelletier/go-toml/v2"
)

type PackSummary struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Author        string    `json:"author"`
	Version       string    `json:"version"`
	MCVersion     string    `json:"mc_version"`
	ModLoader     string    `json:"mod_loader"`
	LoaderVersion string    `json:"loader_version"`
	ModCount      int       `json:"mod_count"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ModItem struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	FileName    string `json:"file_name"`
	Side        string `json:"side"` // "both", "client", "server"
	Platform    string `json:"platform"`
	ProjectID   string `json:"project_id"`
	VersionID   string `json:"version_id"`
	DownloadURL string `json:"download_url"`
	FileSize    int64  `json:"file_size"`
	Pinned      bool   `json:"pinned"`
}

type Pack struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Author        string    `json:"author"`
	Version       string    `json:"version"`
	MCVersion     string    `json:"mc_version"`
	ModLoader     string    `json:"mod_loader"`
	LoaderVersion string    `json:"loader_version"`
	UpdatedAt     time.Time `json:"updated_at"`
	Mods          []ModItem `json:"mods"`
}

// PackTOML format
type tomlPack struct {
	Name       string                 `toml:"name"`
	Author     string                 `toml:"author"`
	Version    string                 `toml:"version"`
	PackFormat string                 `toml:"pack-format"`
	Index      tomlPackIndex          `toml:"index"`
	Versions   map[string]string      `toml:"versions"`
}

type tomlPackIndex struct {
	File       string `toml:"file"`
	HashFormat string `toml:"hash-format"`
	Hash       string `toml:"hash"`
}

type tomlIndexFile struct {
	File     string `toml:"file"`
	Hash     string `toml:"hash"`
	Metafile bool   `toml:"metafile"`
}

type tomlIndex struct {
	HashFormat string          `toml:"hash-format"`
	Files      []tomlIndexFile `toml:"files"`
}

type tomlModDownload struct {
	URL        string `toml:"url"`
	HashFormat string `toml:"hash-format"`
	Hash       string `toml:"hash"`
}

type tomlModUpdateModrinth struct {
	ModID   string `toml:"mod-id,omitempty"`
	Version string `toml:"version,omitempty"`
}

type tomlModUpdateCurseforge struct {
	FileID    int `toml:"file-id,omitempty"`
	ProjectID int `toml:"project-id,omitempty"`
}

type tomlModUpdate struct {
	Modrinth   *tomlModUpdateModrinth   `toml:"modrinth,omitempty"`
	Curseforge *tomlModUpdateCurseforge `toml:"curseforge,omitempty"`
}

type tomlMod struct {
	Name     string          `toml:"name"`
	FileName string          `toml:"filename"`
	Side     string          `toml:"side"`
	Download tomlModDownload `toml:"download"`
	Update   tomlModUpdate   `toml:"update"`
	Option   *struct {
		Pinned bool `toml:"pinned,omitempty"`
	} `toml:"option,omitempty"`
}

type Manager struct {
	baseDir    string
	log        *logger.Logger
	mu         sync.RWMutex
	httpClient *http.Client
}

func NewManager(baseDir string, log *logger.Logger) *Manager {
	_ = os.MkdirAll(baseDir, 0755)
	return &Manager{
		baseDir:    baseDir,
		log:        log,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (m *Manager) packDir(id string) string {
	return filepath.Join(m.baseDir, id)
}

func (m *Manager) ListPacks() ([]PackSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []PackSummary{}, nil
		}
		return nil, err
	}

	var list []PackSummary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pack, err := m.readPack(entry.Name())
		if err != nil {
			continue
		}
		list = append(list, PackSummary{
			ID:            pack.ID,
			Name:          pack.Name,
			Author:        pack.Author,
			Version:       pack.Version,
			MCVersion:     pack.MCVersion,
			ModLoader:     pack.ModLoader,
			LoaderVersion: pack.LoaderVersion,
			ModCount:      len(pack.Mods),
			UpdatedAt:     pack.UpdatedAt,
		})
	}

	return list, nil
}

func (m *Manager) GetPack(id string) (*Pack, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.readPack(id)
}

func (m *Manager) CreatePack(p *Pack) (*Pack, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	if p.Name == "" {
		p.Name = "New Modpack"
	}
	if p.Version == "" {
		p.Version = "1.0.0"
	}
	if p.MCVersion == "" {
		p.MCVersion = "1.20.1"
	}
	if p.ModLoader == "" {
		p.ModLoader = "fabric"
	}
	p.UpdatedAt = time.Now()

	pDir := m.packDir(p.ID)
	modsDir := filepath.Join(pDir, "mods")
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create pack directory: %w", err)
	}

	if err := m.writePackFiles(p); err != nil {
		return nil, err
	}

	return p, nil
}

func (m *Manager) UpdatePack(p *Pack) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pDir := m.packDir(p.ID)
	if _, err := os.Stat(pDir); err != nil {
		return fmt.Errorf("pack not found: %w", err)
	}

	p.UpdatedAt = time.Now()
	return m.writePackFiles(p)
}

func (m *Manager) DeletePack(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return os.RemoveAll(m.packDir(id))
}

func (m *Manager) AddMod(packID string, mod ModItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pack, err := m.readPack(packID)
	if err != nil {
		return err
	}

	if mod.Side == "" {
		mod.Side = "both"
	}
	if mod.Slug == "" {
		mod.Slug = sanitizeSlug(mod.Name)
	}

	// Check if already exists; if so, replace
	found := false
	for i, item := range pack.Mods {
		if item.Slug == mod.Slug {
			pack.Mods[i] = mod
			found = true
			break
		}
	}
	if !found {
		pack.Mods = append(pack.Mods, mod)
	}

	pack.UpdatedAt = time.Now()
	return m.writePackFiles(pack)
}

func (m *Manager) UpdateMod(packID, slug, side string, pinned bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pack, err := m.readPack(packID)
	if err != nil {
		return err
	}

	found := false
	for i, item := range pack.Mods {
		if item.Slug == slug {
			if side != "" {
				pack.Mods[i].Side = side
			}
			pack.Mods[i].Pinned = pinned
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("mod not found in pack")
	}

	pack.UpdatedAt = time.Now()
	return m.writePackFiles(pack)
}

func (m *Manager) DeleteMod(packID, slug string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pack, err := m.readPack(packID)
	if err != nil {
		return err
	}

	var newMods []ModItem
	for _, item := range pack.Mods {
		if item.Slug != slug {
			newMods = append(newMods, item)
		}
	}
	pack.Mods = newMods
	pack.UpdatedAt = time.Now()

	// Remove mod toml file
	_ = os.Remove(filepath.Join(m.packDir(packID), "mods", slug+".pw.toml"))

	return m.writePackFiles(pack)
}

func (m *Manager) readPack(id string) (*Pack, error) {
	pDir := m.packDir(id)
	packPath := filepath.Join(pDir, "pack.toml")
	data, err := os.ReadFile(packPath)
	if err != nil {
		return nil, err
	}

	var tp tomlPack
	if err := toml.Unmarshal(data, &tp); err != nil {
		return nil, err
	}

	info, _ := os.Stat(packPath)
	updatedAt := time.Now()
	if info != nil {
		updatedAt = info.ModTime()
	}

	mcVer := tp.Versions["minecraft"]
	loader := "fabric"
	loaderVer := ""
	for k, v := range tp.Versions {
		if k != "minecraft" {
			loader = k
			loaderVer = v
			break
		}
	}

	// Read mods from mods/
	modsDir := filepath.Join(pDir, "mods")
	var mods []ModItem
	if files, err := os.ReadDir(modsDir); err == nil {
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".pw.toml") {
				mBytes, err := os.ReadFile(filepath.Join(modsDir, f.Name()))
				if err != nil {
					continue
				}
				var tm tomlMod
				if err := toml.Unmarshal(mBytes, &tm); err != nil {
					continue
				}

				slug := strings.TrimSuffix(f.Name(), ".pw.toml")
				platform := "modrinth"
				projID := ""
				verID := ""
				if tm.Update.Curseforge != nil && tm.Update.Curseforge.ProjectID > 0 {
					platform = "curseforge"
					projID = strconv.Itoa(tm.Update.Curseforge.ProjectID)
					verID = strconv.Itoa(tm.Update.Curseforge.FileID)
				} else if tm.Update.Modrinth != nil {
					projID = tm.Update.Modrinth.ModID
					verID = tm.Update.Modrinth.Version
				}

				pinned := false
				if tm.Option != nil {
					pinned = tm.Option.Pinned
				}

				mods = append(mods, ModItem{
					Slug:        slug,
					Name:        tm.Name,
					FileName:    tm.FileName,
					Side:        tm.Side,
					Platform:    platform,
					ProjectID:   projID,
					VersionID:   verID,
					DownloadURL: tm.Download.URL,
					Pinned:      pinned,
				})
			}
		}
	}

	return &Pack{
		ID:            id,
		Name:          tp.Name,
		Author:        tp.Author,
		Version:       tp.Version,
		MCVersion:     mcVer,
		ModLoader:     loader,
		LoaderVersion: loaderVer,
		UpdatedAt:     updatedAt,
		Mods:          mods,
	}, nil
}

func (m *Manager) writePackFiles(p *Pack) error {
	pDir := m.packDir(p.ID)
	modsDir := filepath.Join(pDir, "mods")
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		return err
	}

	// 1. Write individual mod tomls and collect for index
	var indexFiles []tomlIndexFile
	for _, mod := range p.Mods {
		slug := mod.Slug
		if slug == "" {
			slug = sanitizeSlug(mod.Name)
		}

		tm := tomlMod{
			Name:     mod.Name,
			FileName: mod.FileName,
			Side:     mod.Side,
			Download: tomlModDownload{
				URL:        mod.DownloadURL,
				HashFormat: "sha256",
			},
			Option: &struct {
				Pinned bool `toml:"pinned,omitempty"`
			}{
				Pinned: mod.Pinned,
			},
		}

		if mod.Platform == "curseforge" {
			pID, _ := strconv.Atoi(mod.ProjectID)
			fID, _ := strconv.Atoi(mod.VersionID)
			tm.Update.Curseforge = &tomlModUpdateCurseforge{
				ProjectID: pID,
				FileID:    fID,
			}
		} else {
			tm.Update.Modrinth = &tomlModUpdateModrinth{
				ModID:   mod.ProjectID,
				Version: mod.VersionID,
			}
		}

		modBytes, err := toml.Marshal(tm)
		if err != nil {
			return err
		}

		modFilePath := filepath.Join(modsDir, slug+".pw.toml")
		if err := os.WriteFile(modFilePath, modBytes, 0644); err != nil {
			return err
		}

		// Calculate hash for index
		h := sha256.Sum256(modBytes)
		indexFiles = append(indexFiles, tomlIndexFile{
			File:     "mods/" + slug + ".pw.toml",
			Hash:     hex.EncodeToString(h[:]),
			Metafile: true,
		})
	}

	// 2. Write index.toml
	idx := tomlIndex{
		HashFormat: "sha256",
		Files:      indexFiles,
	}
	idxBytes, err := toml.Marshal(idx)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(pDir, "index.toml"), idxBytes, 0644); err != nil {
		return err
	}

	idxHash := sha256.Sum256(idxBytes)

	// 3. Write pack.toml
	versions := map[string]string{
		"minecraft": p.MCVersion,
	}
	if p.ModLoader != "" {
		loaderVer := p.LoaderVersion
		if loaderVer == "" {
			loaderVer = "latest"
		}
		versions[strings.ToLower(p.ModLoader)] = loaderVer
	}

	tp := tomlPack{
		Name:       p.Name,
		Author:     p.Author,
		Version:    p.Version,
		PackFormat: "packwiz:1.1.0",
		Index: tomlPackIndex{
			File:       "index.toml",
			HashFormat: "sha256",
			Hash:       hex.EncodeToString(idxHash[:]),
		},
		Versions: versions,
	}

	packBytes, err := toml.Marshal(tp)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(pDir, "pack.toml"), packBytes, 0644)
}

func (m *Manager) ServePackFile(packID, relativePath string) ([]byte, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cleanRel := filepath.Clean(relativePath)
	cleanRel = strings.TrimPrefix(cleanRel, "/")
	if strings.Contains(cleanRel, "..") {
		return nil, "", fmt.Errorf("invalid path")
	}

	fullPath := filepath.Join(m.packDir(packID), cleanRel)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, "", err
	}

	contentType := "text/plain; charset=utf-8"
	if strings.HasSuffix(fullPath, ".toml") {
		contentType = "application/toml; charset=utf-8"
	}
	return data, contentType, nil
}

// ExportMrpack generates a Modrinth .mrpack archive
func (m *Manager) ExportMrpack(packID string, w io.Writer) error {
	pack, err := m.GetPack(packID)
	if err != nil {
		return err
	}

	zw := zip.NewWriter(w)
	defer zw.Close()

	type mrpackFile struct {
		Path      string            `json:"path"`
		Hashes    map[string]string `json:"hashes"`
		Env       map[string]string `json:"env"`
		Downloads []string          `json:"downloads"`
		FileSize  int64             `json:"fileSize,omitempty"`
	}

	var files []mrpackFile
	for _, mod := range pack.Mods {
		clientEnv := "required"
		serverEnv := "required"
		if mod.Side == "client" {
			serverEnv = "unsupported"
		} else if mod.Side == "server" {
			clientEnv = "unsupported"
		}

		files = append(files, mrpackFile{
			Path: "mods/" + mod.FileName,
			Hashes: map[string]string{
				"sha512": hex.EncodeToString(sha512.New().Sum(nil)),
			},
			Env: map[string]string{
				"client": clientEnv,
				"server": serverEnv,
			},
			Downloads: []string{mod.DownloadURL},
			FileSize:  mod.FileSize,
		})
	}

	deps := map[string]string{
		"minecraft": pack.MCVersion,
	}
	loaderKey := strings.ToLower(pack.ModLoader)
	if loaderKey == "fabric" {
		deps["fabric-loader"] = pack.LoaderVersion
	} else if loaderKey == "forge" {
		deps["forge"] = pack.LoaderVersion
	} else if loaderKey == "neoforge" {
		deps["neoforge"] = pack.LoaderVersion
	} else if loaderKey == "quilt" {
		deps["quilt-loader"] = pack.LoaderVersion
	}

	mrIndex := map[string]any{
		"formatVersion": 1,
		"game":          "minecraft",
		"versionId":     pack.Version,
		"name":          pack.Name,
		"summary":       fmt.Sprintf("%s modpack created in DiscoPanel Studio", pack.Name),
		"files":         files,
		"dependencies":  deps,
	}

	idxBytes, err := json.MarshalIndent(mrIndex, "", "  ")
	if err != nil {
		return err
	}

	f, err := zw.Create("modrinth.index.json")
	if err != nil {
		return err
	}
	if _, err := f.Write(idxBytes); err != nil {
		return err
	}

	// Create overrides dir
	_, _ = zw.Create("overrides/")
	return nil
}

// ExportCurseForge generates a CurseForge .zip modpack
func (m *Manager) ExportCurseForge(packID string, w io.Writer) error {
	pack, err := m.GetPack(packID)
	if err != nil {
		return err
	}

	zw := zip.NewWriter(w)
	defer zw.Close()

	type cfFile struct {
		ProjectID int  `json:"projectID"`
		FileID    int  `json:"fileID"`
		Required  bool `json:"required"`
	}

	var cfFiles []cfFile
	var modlistItems []string

	for _, mod := range pack.Mods {
		pID, _ := strconv.Atoi(mod.ProjectID)
		fID, _ := strconv.Atoi(mod.VersionID)
		if pID > 0 && fID > 0 {
			cfFiles = append(cfFiles, cfFile{
				ProjectID: pID,
				FileID:    fID,
				Required:  mod.Side != "client",
			})
		}
		modlistItems = append(modlistItems, fmt.Sprintf("<li><a href=\"%s\">%s</a> (%s)</li>", mod.DownloadURL, mod.Name, mod.Side))
	}

	loaderID := fmt.Sprintf("%s-%s", strings.ToLower(pack.ModLoader), pack.LoaderVersion)
	manifest := map[string]any{
		"minecraft": map[string]any{
			"version": pack.MCVersion,
			"modLoaders": []map[string]any{
				{"id": loaderID, "primary": true},
			},
		},
		"manifestType":    "minecraftModpack",
		"manifestVersion": 1,
		"name":            pack.Name,
		"version":         pack.Version,
		"author":          pack.Author,
		"files":           cfFiles,
		"overrides":       "overrides",
	}

	mBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	f, err := zw.Create("manifest.json")
	if err != nil {
		return err
	}
	if _, err := f.Write(mBytes); err != nil {
		return err
	}

	modlistHTML := fmt.Sprintf("<ul>\n%s\n</ul>", strings.Join(modlistItems, "\n"))
	ml, err := zw.Create("modlist.html")
	if err == nil {
		_, _ = ml.Write([]byte(modlistHTML))
	}

	_, _ = zw.Create("overrides/")
	return nil
}

// BakeToServer extracts server-side mods directly into /data/mods
func (m *Manager) BakeToServer(packID, modsDir string) (int, error) {
	pack, err := m.GetPack(packID)
	if err != nil {
		return 0, err
	}

	if err := os.MkdirAll(modsDir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create mods directory: %w", err)
	}

	installed := 0
	for _, mod := range pack.Mods {
		// Server side only or both
		if mod.Side == "client" {
			continue
		}
		if mod.DownloadURL == "" || mod.FileName == "" {
			continue
		}

		targetFile := filepath.Join(modsDir, mod.FileName)

		// Download directly
		resp, err := m.httpClient.Get(mod.DownloadURL)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		out, err := os.Create(targetFile)
		if err != nil {
			resp.Body.Close()
			continue
		}

		_, copyErr := io.Copy(out, resp.Body)
		resp.Body.Close()
		out.Close()

		if copyErr == nil {
			installed++
		}
	}

	return installed, nil
}

func sanitizeSlug(name string) string {
	lower := strings.ToLower(name)
	var sb strings.Builder
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			sb.WriteRune(r)
		} else if r == ' ' {
			sb.WriteRune('-')
		}
	}
	res := sb.String()
	if res == "" {
		res = "mod-" + uuid.New().String()[:8]
	}
	return res
}
