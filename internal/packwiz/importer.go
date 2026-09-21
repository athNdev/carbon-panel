package packwiz

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	toml "github.com/pelletier/go-toml/v2"

	"github.com/athNdev/carbon-panel/pkg/files"
)

// ImportOptions defines settings for modpack import
type ImportOptions struct {
	Name          string `json:"name"`
	Author        string `json:"author"`
	Version       string `json:"version"`
	MCVersion     string `json:"mc_version"`
	ModLoader     string `json:"mod_loader"`
	LoaderVersion string `json:"loader_version"`
}

// ImportMrpack imports a Modrinth .mrpack archive into a new Packwiz project
func (m *Manager) ImportMrpack(r io.ReaderAt, size int64, opts ImportOptions) (*Pack, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("failed to open mrpack zip: %w", err)
	}

	var indexFile *zip.File
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			indexFile = f
			break
		}
	}
	if indexFile == nil {
		return nil, fmt.Errorf("invalid .mrpack: modrinth.index.json not found")
	}

	rc, err := indexFile.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to read modrinth.index.json: %w", err)
	}
	defer rc.Close()

	type mrpackIndex struct {
		FormatVersion int               `json:"formatVersion"`
		Game          string            `json:"game"`
		VersionID     string            `json:"versionId"`
		Name          string            `json:"name"`
		Summary       string            `json:"summary"`
		Dependencies  map[string]string `json:"dependencies"`
		Files         []struct {
			Path      string            `json:"path"`
			Hashes    map[string]string `json:"hashes"`
			Env       map[string]string `json:"env"`
			Downloads []string          `json:"downloads"`
			FileSize  int64             `json:"fileSize"`
		} `json:"files"`
	}

	var mrIdx mrpackIndex
	if err := json.NewDecoder(rc).Decode(&mrIdx); err != nil {
		return nil, fmt.Errorf("failed to parse modrinth.index.json: %w", err)
	}

	packID := uuid.New().String()
	packName := opts.Name
	if packName == "" {
		packName = mrIdx.Name
	}
	if packName == "" {
		packName = "Imported Modpack"
	}

	mcVer := opts.MCVersion
	if mcVer == "" && mrIdx.Dependencies != nil {
		mcVer = mrIdx.Dependencies["minecraft"]
	}
	if mcVer == "" {
		mcVer = "1.20.1"
	}

	loader := opts.ModLoader
	loaderVer := opts.LoaderVersion
	if loader == "" && mrIdx.Dependencies != nil {
		for dep, ver := range mrIdx.Dependencies {
			depLower := strings.ToLower(dep)
			if strings.Contains(depLower, "fabric") {
				loader = "fabric"
				loaderVer = ver
				break
			} else if strings.Contains(depLower, "neoforge") {
				loader = "neoforge"
				loaderVer = ver
				break
			} else if strings.Contains(depLower, "forge") {
				loader = "forge"
				loaderVer = ver
				break
			} else if strings.Contains(depLower, "quilt") {
				loader = "quilt"
				loaderVer = ver
				break
			}
		}
	}
	if loader == "" {
		loader = "fabric"
	}
	if loaderVer == "" {
		loaderVer = "latest"
	}

	packVersion := opts.Version
	if packVersion == "" {
		packVersion = mrIdx.VersionID
	}
	if packVersion == "" {
		packVersion = "1.0.0"
	}

	author := opts.Author
	if author == "" {
		author = "Imported"
	}

	pDir := m.packDir(packID)
	modsDir := filepath.Join(pDir, "mods")
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		return nil, err
	}

	// 1. Extract overrides into pack root
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "overrides/") && !f.FileInfo().IsDir() {
			rel := strings.TrimPrefix(f.Name, "overrides/")
			targetPath := filepath.Join(pDir, rel)
			if !files.Within(pDir, targetPath) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				continue
			}
			frc, err := f.Open()
			if err != nil {
				continue
			}
			out, err := os.Create(targetPath)
			if err != nil {
				frc.Close()
				continue
			}
			_, _ = io.Copy(out, frc)
			frc.Close()
			out.Close()
		}
	}

	// 2. Parse mods and generate .pw.toml files
	var mods []ModItem
	for _, file := range mrIdx.Files {
		if !strings.HasPrefix(file.Path, "mods/") {
			continue
		}

		fileName := filepath.Base(file.Path)
		rawSlug := strings.TrimSuffix(fileName, filepath.Ext(fileName))
		slug := sanitizeSlug(rawSlug)

		side := "both"
		if file.Env != nil {
			if file.Env["client"] == "unsupported" {
				side = "server"
			} else if file.Env["server"] == "unsupported" {
				side = "client"
			}
		}

		dlURL := ""
		if len(file.Downloads) > 0 {
			dlURL = file.Downloads[0]
		}

		hash := ""
		hashFormat := "sha256"
		if h, ok := file.Hashes["sha256"]; ok && h != "" {
			hash = h
			hashFormat = "sha256"
		} else if h, ok := file.Hashes["sha512"]; ok && h != "" {
			hash = h
			hashFormat = "sha512"
		} else if h, ok := file.Hashes["sha1"]; ok && h != "" {
			hash = h
			hashFormat = "sha1"
		}

		modItem := ModItem{
			Slug:        slug,
			Name:        rawSlug,
			FileName:    fileName,
			Side:        side,
			Platform:    "modrinth",
			ProjectID:   slug,
			VersionID:   hash,
			DownloadURL: dlURL,
			FileSize:    file.FileSize,
			Pinned:      false,
		}
		mods = append(mods, modItem)

		tm := tomlMod{
			Name:     modItem.Name,
			FileName: modItem.FileName,
			Side:     modItem.Side,
			Download: tomlModDownload{
				URL:        dlURL,
				HashFormat: hashFormat,
				Hash:       hash,
			},
			Update: tomlModUpdate{
				Modrinth: &tomlModUpdateModrinth{
					ModID:   slug,
					Version: hash,
				},
			},
			Option: &struct {
				Pinned bool `toml:"pinned,omitempty"`
			}{
				Pinned: false,
			},
		}

		modBytes, err := toml.Marshal(tm)
		if err == nil {
			_ = os.WriteFile(filepath.Join(modsDir, slug+".pw.toml"), modBytes, 0644)
		}
	}

	pack := &Pack{
		ID:            packID,
		Name:          packName,
		Author:        author,
		Version:       packVersion,
		MCVersion:     mcVer,
		ModLoader:     loader,
		LoaderVersion: loaderVer,
		UpdatedAt:     time.Now(),
		Mods:          mods,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.writePackFiles(pack); err != nil {
		return nil, err
	}

	return pack, nil
}

// ImportCurseForge imports a CurseForge .zip modpack into a new Packwiz project
func (m *Manager) ImportCurseForge(r io.ReaderAt, size int64, opts ImportOptions) (*Pack, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("failed to open curseforge zip: %w", err)
	}

	var manifestFile *zip.File
	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			manifestFile = f
			break
		}
	}
	if manifestFile == nil {
		return nil, fmt.Errorf("invalid CurseForge pack: manifest.json not found")
	}

	rc, err := manifestFile.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest.json: %w", err)
	}
	defer rc.Close()

	type cfManifest struct {
		Minecraft struct {
			Version    string `json:"version"`
			ModLoaders []struct {
				ID      string `json:"id"`
				Primary bool   `json:"primary"`
			} `json:"modLoaders"`
		} `json:"minecraft"`
		ManifestType    string `json:"manifestType"`
		ManifestVersion int    `json:"manifestVersion"`
		Name            string `json:"name"`
		Version         string `json:"version"`
		Author          string `json:"author"`
		Files           []struct {
			ProjectID int  `json:"projectID"`
			FileID    int  `json:"fileID"`
			Required  bool `json:"required"`
		} `json:"files"`
		Overrides string `json:"overrides"`
	}

	var manifest cfManifest
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest.json: %w", err)
	}

	packID := uuid.New().String()
	packName := opts.Name
	if packName == "" {
		packName = manifest.Name
	}
	if packName == "" {
		packName = "Imported CurseForge Pack"
	}

	mcVer := opts.MCVersion
	if mcVer == "" {
		mcVer = manifest.Minecraft.Version
	}
	if mcVer == "" {
		mcVer = "1.20.1"
	}

	loader := opts.ModLoader
	loaderVer := opts.LoaderVersion
	if loader == "" && len(manifest.Minecraft.ModLoaders) > 0 {
		rawLoader := manifest.Minecraft.ModLoaders[0].ID
		parts := strings.SplitN(rawLoader, "-", 2)
		if len(parts) >= 1 {
			loader = strings.ToLower(parts[0])
		}
		if len(parts) >= 2 {
			loaderVer = parts[1]
		}
	}
	if loader == "" {
		loader = "forge"
	}
	if loaderVer == "" {
		loaderVer = "latest"
	}

	packVersion := opts.Version
	if packVersion == "" {
		packVersion = manifest.Version
	}
	if packVersion == "" {
		packVersion = "1.0.0"
	}

	author := opts.Author
	if author == "" {
		author = manifest.Author
	}
	if author == "" {
		author = "Imported"
	}

	pDir := m.packDir(packID)
	modsDir := filepath.Join(pDir, "mods")
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		return nil, err
	}

	// Extract overrides
	overridesDir := manifest.Overrides
	if overridesDir == "" {
		overridesDir = "overrides"
	}
	prefix := strings.TrimSuffix(overridesDir, "/") + "/"

	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, prefix) && !f.FileInfo().IsDir() {
			rel := strings.TrimPrefix(f.Name, prefix)
			targetPath := filepath.Join(pDir, rel)
			if !files.Within(pDir, targetPath) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				continue
			}
			frc, err := f.Open()
			if err != nil {
				continue
			}
			out, err := os.Create(targetPath)
			if err != nil {
				frc.Close()
				continue
			}
			_, _ = io.Copy(out, frc)
			frc.Close()
			out.Close()
		}
	}

	// Register CurseForge mods
	var mods []ModItem
	for _, file := range manifest.Files {
		slug := fmt.Sprintf("cf-%d", file.ProjectID)
		name := fmt.Sprintf("CurseForge Mod %d", file.ProjectID)
		fileName := fmt.Sprintf("cf-%d-%d.jar", file.ProjectID, file.FileID)

		side := "both"
		if !file.Required {
			side = "client"
		}

		modItem := ModItem{
			Slug:        slug,
			Name:        name,
			FileName:    fileName,
			Side:        side,
			Platform:    "curseforge",
			ProjectID:   strconv.Itoa(file.ProjectID),
			VersionID:   strconv.Itoa(file.FileID),
			DownloadURL: "",
			Pinned:      false,
		}
		mods = append(mods, modItem)

		tm := tomlMod{
			Name:     modItem.Name,
			FileName: modItem.FileName,
			Side:     modItem.Side,
			Download: tomlModDownload{
				URL:        "",
				HashFormat: "sha256",
				Hash:       "",
			},
			Update: tomlModUpdate{
				Curseforge: &tomlModUpdateCurseforge{
					ProjectID: file.ProjectID,
					FileID:    file.FileID,
				},
			},
			Option: &struct {
				Pinned bool `toml:"pinned,omitempty"`
			}{
				Pinned: false,
			},
		}

		modBytes, err := toml.Marshal(tm)
		if err == nil {
			_ = os.WriteFile(filepath.Join(modsDir, slug+".pw.toml"), modBytes, 0644)
		}
	}

	pack := &Pack{
		ID:            packID,
		Name:          packName,
		Author:        author,
		Version:       packVersion,
		MCVersion:     mcVer,
		ModLoader:     loader,
		LoaderVersion: loaderVer,
		UpdatedAt:     time.Now(),
		Mods:          mods,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.writePackFiles(pack); err != nil {
		return nil, err
	}

	return pack, nil
}

// ImportPackwizZip imports a native Packwiz archive (.zip containing pack.toml, index.toml, mods/)
func (m *Manager) ImportPackwizZip(r io.ReaderAt, size int64, opts ImportOptions) (*Pack, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("failed to open packwiz zip: %w", err)
	}

	// Check if pack.toml exists
	var rootPrefix string
	var hasPackToml bool
	for _, f := range zr.File {
		if f.Name == "pack.toml" {
			hasPackToml = true
			rootPrefix = ""
			break
		} else if strings.HasSuffix(f.Name, "/pack.toml") && strings.Count(f.Name, "/") == 1 {
			hasPackToml = true
			rootPrefix = strings.TrimSuffix(f.Name, "pack.toml")
			break
		}
	}

	if !hasPackToml {
		return nil, fmt.Errorf("invalid Packwiz archive: pack.toml not found")
	}

	packID := uuid.New().String()
	pDir := m.packDir(packID)
	if err := os.MkdirAll(pDir, 0755); err != nil {
		return nil, err
	}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rel := f.Name
		if rootPrefix != "" {
			if !strings.HasPrefix(rel, rootPrefix) {
				continue
			}
			rel = strings.TrimPrefix(rel, rootPrefix)
		}

		targetPath := filepath.Join(pDir, rel)

		if !files.Within(pDir, targetPath) {

			continue

		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			continue
		}
		out, err := os.Create(targetPath)
		if err != nil {
			rc.Close()
			continue
		}
		_, _ = io.Copy(out, rc)
		rc.Close()
		out.Close()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	pack, err := m.readPack(packID)
	if err != nil {
		_ = os.RemoveAll(pDir)
		return nil, fmt.Errorf("failed to load imported pack: %w", err)
	}

	if opts.Name != "" {
		pack.Name = opts.Name
	}
	if opts.Author != "" {
		pack.Author = opts.Author
	}
	if opts.Version != "" {
		pack.Version = opts.Version
	}
	pack.UpdatedAt = time.Now()

	if err := m.writePackFiles(pack); err != nil {
		return nil, err
	}

	return pack, nil
}
