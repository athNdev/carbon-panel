package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/minecraft"
	"github.com/athNdev/carbon-panel/pkg/files"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1/carbonpanelv1connect"
	"github.com/athNdev/carbon-panel/pkg/upload"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check that ModService implements the interface
var _ carbonpanelv1connect.ModServiceHandler = (*ModService)(nil)

// ModService implements the Mod service
type ModService struct {
	store         *storage.Store
	docker        *docker.Client
	log           *logger.Logger
	uploadManager *upload.Manager
}

// NewModService creates a new mod service
func NewModService(store *storage.Store, docker *docker.Client, uploadManager *upload.Manager, log *logger.Logger) *ModService {
	return &ModService{
		store:         store,
		docker:        docker,
		log:           log,
		uploadManager: uploadManager,
	}
}

// ListMods lists mods for a server
func (s *ModService) ListMods(ctx context.Context, req *connect.Request[v1.ListModsRequest]) (*connect.Response[v1.ListModsResponse], error) {
	msg := req.Msg

	// Get server to find data path and mod loader
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Get the mods directory path
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("this server type does not support mods"))
	}

	// Check if mods directory exists
	mods := []*v1.Mod{}

	// Read mods from active directory
	if files, err := os.ReadDir(modsDir); err == nil {
		for _, file := range files {
			if file.IsDir() {
				continue
			}

			// Check if this is a valid mod file
			if !minecraft.IsValidModFile(file.Name(), server.ModLoader) {
				continue
			}

			info, err := file.Info()
			if err != nil {
				continue
			}

			// Extract display name from filename (remove extension)
			displayName := file.Name()
			if ext := filepath.Ext(displayName); ext != "" {
				displayName = displayName[:len(displayName)-len(ext)]
			}

			// Create mod entry with consistent ID generation
			mod := &v1.Mod{
				Id:          uuid.NewSHA1(uuid.NameSpaceURL, []byte(msg.ServerId+file.Name())).String(),
				ServerId:    msg.ServerId,
				FileName:    file.Name(),
				DisplayName: displayName,
				Enabled:     true,
				FileSize:    info.Size(),
				UploadedAt:  timestamppb.New(info.ModTime()),
			}

			mods = append(mods, mod)
		}
	}

	// Also check disabled mods directory
	disabledDir := modsDir + "_disabled"
	if files, err := os.ReadDir(disabledDir); err == nil {
		for _, file := range files {
			if file.IsDir() {
				continue
			}

			if !minecraft.IsValidModFile(file.Name(), server.ModLoader) {
				continue
			}

			info, err := file.Info()
			if err != nil {
				continue
			}

			// Extract display name from filename
			displayName := file.Name()
			if ext := filepath.Ext(displayName); ext != "" {
				displayName = displayName[:len(displayName)-len(ext)]
			}

			mod := &v1.Mod{
				Id:          uuid.NewSHA1(uuid.NameSpaceURL, []byte(msg.ServerId+file.Name())).String(),
				ServerId:    msg.ServerId,
				FileName:    file.Name(),
				DisplayName: displayName,
				Enabled:     false,
				FileSize:    info.Size(),
				UploadedAt:  timestamppb.New(info.ModTime()),
			}

			mods = append(mods, mod)
		}
	}

	return connect.NewResponse(&v1.ListModsResponse{
		Mods: mods,
	}), nil
}

// GetMod gets a specific mod
func (s *ModService) GetMod(ctx context.Context, req *connect.Request[v1.GetModRequest]) (*connect.Response[v1.GetModResponse], error) {
	msg := req.Msg

	// Get server to validate and find mod
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Get the mods directory path
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("this server type does not support mods"))
	}

	// Try to find the mod file in active directory
	if files, err := os.ReadDir(modsDir); err == nil {
		for _, file := range files {
			if !file.IsDir() && minecraft.IsValidModFile(file.Name(), server.ModLoader) {
				// Generate the same ID as in ListMods to match
				fileID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(msg.ServerId+file.Name())).String()
				if fileID == msg.ModId {
					info, _ := file.Info()

					displayName := file.Name()
					if ext := filepath.Ext(displayName); ext != "" {
						displayName = displayName[:len(displayName)-len(ext)]
					}

					return connect.NewResponse(&v1.GetModResponse{
						Mod: &v1.Mod{
							Id:          fileID,
							ServerId:    msg.ServerId,
							FileName:    file.Name(),
							DisplayName: displayName,
							Enabled:     true,
							FileSize:    info.Size(),
							UploadedAt:  timestamppb.New(info.ModTime()),
						},
					}), nil
				}
			}
		}
	}

	// Try disabled directory
	disabledDir := modsDir + "_disabled"
	if files, err := os.ReadDir(disabledDir); err == nil {
		for _, file := range files {
			if !file.IsDir() && minecraft.IsValidModFile(file.Name(), server.ModLoader) {
				fileID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(msg.ServerId+file.Name())).String()
				if fileID == msg.ModId {
					info, _ := file.Info()

					displayName := file.Name()
					if ext := filepath.Ext(displayName); ext != "" {
						displayName = displayName[:len(displayName)-len(ext)]
					}

					return connect.NewResponse(&v1.GetModResponse{
						Mod: &v1.Mod{
							Id:          fileID,
							ServerId:    msg.ServerId,
							FileName:    file.Name(),
							DisplayName: displayName,
							Enabled:     false,
							FileSize:    info.Size(),
							UploadedAt:  timestamppb.New(info.ModTime()),
						},
					}), nil
				}
			}
		}
	}

	return nil, connect.NewError(connect.CodeNotFound, errors.New("mod not found"))
}

// ImportUploadedMod imports a mod
func (s *ModService) ImportUploadedMod(ctx context.Context, req *connect.Request[v1.ImportUploadedModRequest]) (*connect.Response[v1.ImportUploadedModResponse], error) {
	msg := req.Msg

	// Validate upload session
	if msg.UploadSessionId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("upload_session_id is required"))
	}

	// Get server to find data path and mod loader
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Get temp file path and original filename from upload manager
	tempPath, originalFilename, err := s.uploadManager.GetTempPath(msg.UploadSessionId)
	if err != nil {
		s.log.Error("Failed to get upload session: %v", err)
		return nil, connect.NewError(connect.CodeNotFound, errors.New("upload session not found or not completed"))
	}

	// Validate file is appropriate for this mod loader
	if !minecraft.IsValidModFile(originalFilename, server.ModLoader) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid file type for this mod loader"))
	}

	// Get the correct mods directory based on mod loader
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("this server type does not support mods"))
	}

	// Create mods directory if needed
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		s.log.Error("Failed to create mods directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create mods directory"))
	}

	// Move file from temp location to mods dir
	modPath := filepath.Join(modsDir, originalFilename)
	if err := os.Rename(tempPath, modPath); err != nil {
		if err := files.CopyFile(tempPath, modPath); err != nil {
			s.log.Error("Failed to move mod file: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to save mod"))
		}
		os.Remove(tempPath)
	}

	// Cleanup the upload session
	s.uploadManager.CleanupSession(msg.UploadSessionId)

	// Get file info for the response
	info, err := os.Stat(modPath)
	if err != nil {
		s.log.Error("Failed to stat mod file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get mod info"))
	}

	// Use provided display name or derive from filename
	displayName := msg.DisplayName
	if displayName == "" {
		displayName = originalFilename
		if ext := filepath.Ext(displayName); ext != "" {
			displayName = displayName[:len(displayName)-len(ext)]
		}
	}

	// Create mod record
	mod := &v1.Mod{
		Id:          uuid.NewSHA1(uuid.NameSpaceURL, []byte(msg.ServerId+originalFilename)).String(),
		ServerId:    msg.ServerId,
		FileName:    originalFilename,
		DisplayName: displayName,
		Description: msg.Description,
		Enabled:     true,
		FileSize:    info.Size(),
		UploadedAt:  timestamppb.New(info.ModTime()),
	}

	return connect.NewResponse(&v1.ImportUploadedModResponse{
		Mod:     mod,
		Message: "Mod uploaded successfully",
	}), nil
}

// UpdateMod updates a mod
func (s *ModService) UpdateMod(ctx context.Context, req *connect.Request[v1.UpdateModRequest]) (*connect.Response[v1.UpdateModResponse], error) {
	msg := req.Msg

	// Get server to find mod path
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("this server type does not support mods"))
	}

	disabledDir := modsDir + "_disabled"

	// Find the mod file
	var modFileName string
	var currentlyEnabled bool
	var modInfo os.FileInfo

	// First, scan the mods directory
	if files, err := os.ReadDir(modsDir); err == nil {
		for _, file := range files {
			if !file.IsDir() && minecraft.IsValidModFile(file.Name(), server.ModLoader) {
				// Generate the same ID as in ListMods to match
				fileID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(msg.ServerId+file.Name())).String()
				if fileID == msg.ModId {
					modFileName = file.Name()
					currentlyEnabled = true
					modInfo, _ = file.Info()
					break
				}
			}
		}
	}

	// If not found, check disabled directory
	if modFileName == "" {
		if files, err := os.ReadDir(disabledDir); err == nil {
			for _, file := range files {
				if !file.IsDir() && minecraft.IsValidModFile(file.Name(), server.ModLoader) {
					fileID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(msg.ServerId+file.Name())).String()
					if fileID == msg.ModId {
						modFileName = file.Name()
						currentlyEnabled = false
						modInfo, _ = file.Info()
						break
					}
				}
			}
		}
	}

	if modFileName == "" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("mod not found"))
	}

	// Handle enabling/disabling
	finalEnabled := currentlyEnabled
	if msg.Enabled != nil && *msg.Enabled != currentlyEnabled {
		if *msg.Enabled {
			// Move from disabled to mods directory
			oldPath := filepath.Join(disabledDir, modFileName)
			newPath := filepath.Join(modsDir, modFileName)
			if err := os.Rename(oldPath, newPath); err != nil {
				s.log.Error("Failed to enable mod: %v", err)
				return nil, connect.NewError(connect.CodeInternal, errors.New("failed to enable mod"))
			}
			finalEnabled = true
		} else {
			// Move from mods to disabled directory
			os.MkdirAll(disabledDir, 0755)
			oldPath := filepath.Join(modsDir, modFileName)
			newPath := filepath.Join(disabledDir, modFileName)
			if err := os.Rename(oldPath, newPath); err != nil {
				s.log.Error("Failed to disable mod: %v", err)
				return nil, connect.NewError(connect.CodeInternal, errors.New("failed to disable mod"))
			}
			finalEnabled = false
		}
	}

	// Build response
	displayName := modFileName
	if ext := filepath.Ext(displayName); ext != "" {
		displayName = displayName[:len(displayName)-len(ext)]
	}

	// Use provided display name if given
	if msg.DisplayName != nil && *msg.DisplayName != "" {
		displayName = *msg.DisplayName
	}

	description := ""
	if msg.Description != nil {
		description = *msg.Description
	}

	return connect.NewResponse(&v1.UpdateModResponse{
		Mod: &v1.Mod{
			Id:          msg.ModId,
			ServerId:    msg.ServerId,
			FileName:    modFileName,
			DisplayName: displayName,
			Description: description,
			Enabled:     finalEnabled,
			FileSize:    modInfo.Size(),
			UploadedAt:  timestamppb.New(modInfo.ModTime()),
			UpdatedAt:   timestamppb.Now(),
		},
	}), nil
}

// DeleteMod deletes a mod
func (s *ModService) DeleteMod(ctx context.Context, req *connect.Request[v1.DeleteModRequest]) (*connect.Response[v1.DeleteModResponse], error) {
	msg := req.Msg

	// Get server to find file path
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("this server type does not support mods"))
	}

	// Try to find and delete the mod file
	deleted := false

	// Check active directory
	if files, err := os.ReadDir(modsDir); err == nil {
		for _, file := range files {
			if !file.IsDir() && minecraft.IsValidModFile(file.Name(), server.ModLoader) {
				fileID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(msg.ServerId+file.Name())).String()
				if fileID == msg.ModId {
					modPath := filepath.Join(modsDir, file.Name())
					if err := os.Remove(modPath); err != nil {
						s.log.Error("Failed to delete mod file: %v", err)
						return nil, connect.NewError(connect.CodeInternal, errors.New("failed to delete mod file"))
					}
					deleted = true
					break
				}
			}
		}
	}

	// Check disabled directory
	if !deleted {
		disabledDir := modsDir + "_disabled"
		if files, err := os.ReadDir(disabledDir); err == nil {
			for _, file := range files {
				if !file.IsDir() && minecraft.IsValidModFile(file.Name(), server.ModLoader) {
					fileID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(msg.ServerId+file.Name())).String()
					if fileID == msg.ModId {
						modPath := filepath.Join(disabledDir, file.Name())
						if err := os.Remove(modPath); err != nil {
							s.log.Error("Failed to delete mod file: %v", err)
							return nil, connect.NewError(connect.CodeInternal, errors.New("failed to delete mod file"))
						}
						deleted = true
						break
					}
				}
			}
		}
	}

	if !deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("mod not found"))
	}

	return connect.NewResponse(&v1.DeleteModResponse{
		Message: "Mod deleted successfully",
	}), nil
}

type modrinthVersionFile struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
}

type modrinthVersionItem struct {
	ID            string                `json:"id"`
	VersionNumber string                `json:"version_number"`
	Files         []modrinthVersionFile `json:"files"`
}

var fabricOptStackDefs = []struct {
	ModID       string
	Name        string
	Description string
	PrefixMatch string
}{
	{ModID: "lithium", Name: "Lithium", Description: "General physics, mob AI, and world ticking optimization", PrefixMatch: "lithium"},
	{ModID: "ferrite-core", Name: "FerriteCore", Description: "Memory usage optimization (reduces RAM overhead)", PrefixMatch: "ferritecore"},
	{ModID: "modernfix", Name: "ModernFix", Description: "All-in-one memory leak, launch time, and allocation optimization", PrefixMatch: "modernfix"},
	{ModID: "c2me-fabric", Name: "C2ME", Description: "Concurrent chunk management and multithreaded world generation", PrefixMatch: "c2me"},
}

func fetchModrinthModVersion(ctx context.Context, slug string, mcVersion string) (*modrinthVersionItem, error) {
	loadersJSON := url.QueryEscape(`["fabric"]`)
	gameVersionsJSON := url.QueryEscape(fmt.Sprintf(`["%s"]`, mcVersion))
	reqURL := fmt.Sprintf("https://api.modrinth.com/v2/project/%s/version?loaders=%s&game_versions=%s", slug, loadersJSON, gameVersionsJSON)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "CarbonPanel/1.0 (contact@carbon-panel.local)")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("modrinth returned status %d", resp.StatusCode)
	}

	var versions []modrinthVersionItem
	if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
		return nil, err
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("no versions found for mc %s", mcVersion)
	}

	return &versions[0], nil
}

// GetFabricOptimizationStack checks the status and compatibility of Lithium, FerriteCore, ModernFix, and C2ME
func (s *ModService) GetFabricOptimizationStack(ctx context.Context, req *connect.Request[v1.GetFabricOptimizationStackRequest]) (*connect.Response[v1.GetFabricOptimizationStackResponse], error) {
	msg := req.Msg
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	isFabric := server.ModLoader == storage.ModLoaderFabric || server.ModLoader == storage.ModLoaderQuilt
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)

	// List files in mods directory to detect installed versions
	installedFiles := make(map[string]string)
	if entries, err := os.ReadDir(modsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jar") {
				lower := strings.ToLower(entry.Name())
				for _, def := range fabricOptStackDefs {
					if strings.Contains(lower, def.PrefixMatch) {
						installedFiles[def.ModID] = entry.Name()
					}
				}
			}
		}
	}

	var resultMods []*v1.FabricOptimizationMod
	for _, def := range fabricOptStackDefs {
		modItem := &v1.FabricOptimizationMod{
			ModId:       def.ModID,
			Name:        def.Name,
			Description: def.Description,
		}

		if fileName, ok := installedFiles[def.ModID]; ok {
			modItem.Installed = true
			modItem.FileName = fileName
			modItem.InstalledVersion = fileName
		}

		if isFabric {
			v, err := fetchModrinthModVersion(ctx, def.ModID, server.MCVersion)
			if err == nil && v != nil && len(v.Files) > 0 {
				modItem.Compatible = true
				modItem.LatestCompatibleVersion = v.VersionNumber
				modItem.DownloadUrl = v.Files[0].URL
				if !modItem.Installed {
					modItem.FileName = v.Files[0].Filename
				}
			} else {
				modItem.Compatible = false
			}
		}

		resultMods = append(resultMods, modItem)
	}

	return connect.NewResponse(&v1.GetFabricOptimizationStackResponse{
		IsFabric:  isFabric,
		McVersion: server.MCVersion,
		Mods:      resultMods,
	}), nil
}

// InstallFabricOptimizationStack installs or updates Lithium, FerriteCore, ModernFix, and/or C2ME
func (s *ModService) InstallFabricOptimizationStack(ctx context.Context, req *connect.Request[v1.InstallFabricOptimizationStackRequest]) (*connect.Response[v1.InstallFabricOptimizationStackResponse], error) {
	msg := req.Msg
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	if server.ModLoader != storage.ModLoaderFabric && server.ModLoader != storage.ModLoaderQuilt {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("server mod loader is not Fabric or Quilt"))
	}

	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create mods directory: %w", err))
	}

	filter := make(map[string]bool)
	for _, id := range msg.ModIds {
		filter[id] = true
	}

	var installedMods []*v1.FabricOptimizationMod
	var warnings []string

	for _, def := range fabricOptStackDefs {
		if len(filter) > 0 && !filter[def.ModID] {
			continue
		}

		v, err := fetchModrinthModVersion(ctx, def.ModID, server.MCVersion)
		if err != nil || v == nil || len(v.Files) == 0 {
			warnings = append(warnings, fmt.Sprintf("%s is not compatible with Minecraft %s: %v", def.Name, server.MCVersion, err))
			continue
		}

		targetFile := v.Files[0]
		targetPath := filepath.Join(modsDir, targetFile.Filename)

		// Remove existing older versions of this mod
		if entries, err := os.ReadDir(modsDir); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && strings.Contains(strings.ToLower(entry.Name()), def.PrefixMatch) && entry.Name() != targetFile.Filename {
					_ = os.Remove(filepath.Join(modsDir, entry.Name()))
				}
			}
		}

		// Download latest compatible version
		resp, err := http.Get(targetFile.URL)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to download %s: %v", def.Name, err))
			continue
		}

		out, err := os.Create(targetPath)
		if err != nil {
			resp.Body.Close()
			warnings = append(warnings, fmt.Sprintf("failed to save %s: %v", def.Name, err))
			continue
		}

		_, copyErr := io.Copy(out, resp.Body)
		resp.Body.Close()
		out.Close()

		if copyErr != nil {
			warnings = append(warnings, fmt.Sprintf("failed while saving %s: %v", def.Name, copyErr))
			continue
		}

		installedMods = append(installedMods, &v1.FabricOptimizationMod{
			ModId:                   def.ModID,
			Name:                    def.Name,
			Description:             def.Description,
			Installed:               true,
			Compatible:              true,
			InstalledVersion:        v.VersionNumber,
			LatestCompatibleVersion: v.VersionNumber,
			FileName:                targetFile.Filename,
			DownloadUrl:             targetFile.URL,
		})
	}

	return connect.NewResponse(&v1.InstallFabricOptimizationStackResponse{
		InstalledMods: installedMods,
		Warnings:      warnings,
		Message:       fmt.Sprintf("Installed %d Fabric optimization mod(s)", len(installedMods)),
	}), nil
}
