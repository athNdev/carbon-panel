package svc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/modrinth"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/nodeagent"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/google/uuid"
)

const (
	maxAddonDownloadSize = 250 * 1024 * 1024 // 250 MB
)

// AddonService implements AddonServiceHandler for plugin and mod lifecycle management.
type AddonService struct {
	deps           Deps
	dispatcher     *AgentDispatcher
	modrinthClient *modrinth.Client
	httpClient     *http.Client
}

// NewAddonService creates a new AddonService instance.
func NewAddonService(deps Deps, dispatcher *AgentDispatcher, modClient *modrinth.Client, httpClient *http.Client) *AddonService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	if modClient == nil {
		modClient = modrinth.NewClient(httpClient)
	}
	return &AddonService{
		deps:           deps,
		dispatcher:     dispatcher,
		modrinthClient: modClient,
		httpClient:     httpClient,
	}
}

func (s *AddonService) loadWorkload(ctx context.Context, id string) (*db.Workload, error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, err
	}
	var w db.Workload
	if err := q.Where("id = ?", id).First(&w).Error; err != nil {
		return nil, errWorkloadNotFound
	}
	return &w, nil
}

func (s *AddonService) checkWorkloadNode(w *db.Workload) error {
	if w.NodeID == "" {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("workload is not placed on any node"))
	}
	if s.dispatcher == nil || !s.dispatcher.IsConnected(w.NodeID) {
		return connect.NewError(connect.CodeUnavailable, errors.New("node agent is offline"))
	}
	return nil
}

func (s *AddonService) scanAddonDir(ctx context.Context, w *db.Workload, dir string, aType v1.AddonType) ([]*v1.InstalledAddon, error) {
	commandID := uuid.NewString()
	waitCh := s.dispatcher.ExpectFileList(commandID)
	defer s.dispatcher.CancelFileList(commandID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_FileList{
			FileList: &v1.ControlFileList{
				CommandId:  commandID,
				WorkloadId: w.ID,
				Path:       dir,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch directory listing to node agent"))
	}

	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(15 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for addon listing from node agent"))
	case res := <-waitCh:
		if !res.Success {
			// If directory does not exist yet (e.g. plugins or mods not created), return empty
			return []*v1.InstalledAddon{}, nil
		}

		var addons []*v1.InstalledAddon
		for _, f := range res.Files {
			if f.IsDir {
				continue
			}

			name := f.Name
			var enabled bool
			var baseName string

			if strings.HasSuffix(name, ".jar") {
				enabled = true
				baseName = strings.TrimSuffix(name, ".jar")
			} else if strings.HasSuffix(name, ".jar.disabled") {
				enabled = false
				baseName = strings.TrimSuffix(name, ".jar.disabled")
			} else {
				continue
			}

			addons = append(addons, &v1.InstalledAddon{
				Filename:       name,
				Name:           baseName,
				AddonType:      aType,
				Enabled:        enabled,
				SizeBytes:      f.Size,
				ModifiedAtUnix: f.ModifiedAtUnix,
			})
		}
		return addons, nil
	}
}

// ListWorkloadAddons lists all plugins/mods installed on a workload.
func (s *AddonService) ListWorkloadAddons(ctx context.Context, req *connect.Request[v1.ListWorkloadAddonsRequest]) (*connect.Response[v1.ListWorkloadAddonsResponse], error) {
	if strings.TrimSpace(req.Msg.WorkloadId) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload_id is required"))
	}

	w, err := s.loadWorkload(ctx, req.Msg.WorkloadId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if err := s.checkWorkloadNode(w); err != nil {
		return nil, err
	}

	var allAddons []*v1.InstalledAddon

	if req.Msg.AddonType == v1.AddonType_ADDON_TYPE_PLUGIN || req.Msg.AddonType == v1.AddonType_ADDON_TYPE_UNSPECIFIED {
		plugins, err := s.scanAddonDir(ctx, w, "plugins", v1.AddonType_ADDON_TYPE_PLUGIN)
		if err != nil {
			return nil, err
		}
		allAddons = append(allAddons, plugins...)
	}

	if req.Msg.AddonType == v1.AddonType_ADDON_TYPE_MOD || req.Msg.AddonType == v1.AddonType_ADDON_TYPE_UNSPECIFIED {
		mods, err := s.scanAddonDir(ctx, w, "mods", v1.AddonType_ADDON_TYPE_MOD)
		if err != nil {
			return nil, err
		}
		allAddons = append(allAddons, mods...)
	}

	return connect.NewResponse(&v1.ListWorkloadAddonsResponse{
		Addons: allAddons,
	}), nil
}

func getAddonDir(aType v1.AddonType) string {
	if aType == v1.AddonType_ADDON_TYPE_MOD {
		return "mods"
	}
	return "plugins"
}

// ToggleAddon enables (.jar) or disables (.jar.disabled) an installed addon.
func (s *AddonService) ToggleAddon(ctx context.Context, req *connect.Request[v1.ToggleAddonRequest]) (*connect.Response[v1.ToggleAddonResponse], error) {
	if strings.TrimSpace(req.Msg.WorkloadId) == "" || strings.TrimSpace(req.Msg.Filename) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload_id and filename are required"))
	}

	w, err := s.loadWorkload(ctx, req.Msg.WorkloadId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if err := s.checkWorkloadNode(w); err != nil {
		return nil, err
	}

	filename := filepath.Base(req.Msg.Filename)
	if strings.Contains(filename, "..") || filename == "." || filename == "/" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid filename"))
	}

	dir := getAddonDir(req.Msg.AddonType)

	var targetFilename string
	var enabled bool
	var baseName string

	if req.Msg.Enable {
		// Enable: remove .disabled
		if strings.HasSuffix(filename, ".disabled") {
			targetFilename = strings.TrimSuffix(filename, ".disabled")
		} else {
			targetFilename = filename
		}
		enabled = true
		baseName = strings.TrimSuffix(targetFilename, ".jar")
	} else {
		// Disable: append .disabled
		if !strings.HasSuffix(filename, ".disabled") {
			targetFilename = filename + ".disabled"
		} else {
			targetFilename = filename
		}
		enabled = false
		baseName = strings.TrimSuffix(strings.TrimSuffix(targetFilename, ".disabled"), ".jar")
	}

	oldPath := filepath.ToSlash(filepath.Join(dir, filename))
	newPath := filepath.ToSlash(filepath.Join(dir, targetFilename))

	if oldPath != newPath {
		commandID := uuid.NewString()
		waitCh := s.dispatcher.ExpectFileRename(commandID)
		defer s.dispatcher.CancelFileRename(commandID)

		ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
			Payload: &v1.ControlMessage_FileRename{
				FileRename: &v1.ControlFileRename{
					CommandId:  commandID,
					WorkloadId: w.ID,
					OldPath:    oldPath,
					NewPath:    newPath,
				},
			},
		})
		if !ok {
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch rename to node agent"))
		}

		select {
		case <-ctx.Done():
			return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
		case <-time.After(15 * time.Second):
			return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for rename from node agent"))
		case res := <-waitCh:
			if !res.Success {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to toggle addon: %s", res.Error))
			}
		}
	}

	// Query stat of the new path
	statCmdID := uuid.NewString()
	statWaitCh := s.dispatcher.ExpectFileStat(statCmdID)
	defer s.dispatcher.CancelFileStat(statCmdID)

	s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_FileStat{
			FileStat: &v1.ControlStatFile{
				CommandId:  statCmdID,
				WorkloadId: w.ID,
				Path:       newPath,
			},
		},
	})

	var size int64
	var modTime int64
	select {
	case res := <-statWaitCh:
		if res != nil && res.Success && res.Info != nil {
			size = res.Info.Size
			modTime = res.Info.ModifiedAtUnix
		}
	case <-time.After(2 * time.Second):
	}

	return connect.NewResponse(&v1.ToggleAddonResponse{
		Addon: &v1.InstalledAddon{
			Filename:       targetFilename,
			Name:           baseName,
			AddonType:      req.Msg.AddonType,
			Enabled:        enabled,
			SizeBytes:      size,
			ModifiedAtUnix: modTime,
		},
	}), nil
}

// UninstallAddon removes an addon file from disk.
func (s *AddonService) UninstallAddon(ctx context.Context, req *connect.Request[v1.UninstallAddonRequest]) (*connect.Response[v1.UninstallAddonResponse], error) {
	if strings.TrimSpace(req.Msg.WorkloadId) == "" || strings.TrimSpace(req.Msg.Filename) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload_id and filename are required"))
	}

	w, err := s.loadWorkload(ctx, req.Msg.WorkloadId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if err := s.checkWorkloadNode(w); err != nil {
		return nil, err
	}

	filename := filepath.Base(req.Msg.Filename)
	if strings.Contains(filename, "..") || filename == "." || filename == "/" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid filename"))
	}

	dir := getAddonDir(req.Msg.AddonType)
	targetPath := filepath.ToSlash(filepath.Join(dir, filename))

	commandID := uuid.NewString()
	waitCh := s.dispatcher.ExpectFileDelete(commandID)
	defer s.dispatcher.CancelFileDelete(commandID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_FileDelete{
			FileDelete: &v1.ControlDeleteFile{
				CommandId:  commandID,
				WorkloadId: w.ID,
				Path:       targetPath,
				Recursive:  false,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch delete to node agent"))
	}

	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(15 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for delete from node agent"))
	case res := <-waitCh:
		if !res.Success {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete addon: %s", res.Error))
		}
		return connect.NewResponse(&v1.UninstallAddonResponse{}), nil
	}
}

// InstallAddon downloads and installs a plugin or mod into the workload.
func (s *AddonService) InstallAddon(ctx context.Context, req *connect.Request[v1.InstallAddonRequest]) (*connect.Response[v1.InstallAddonResponse], error) {
	if strings.TrimSpace(req.Msg.WorkloadId) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload_id is required"))
	}

	w, err := s.loadWorkload(ctx, req.Msg.WorkloadId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if err := s.checkWorkloadNode(w); err != nil {
		return nil, err
	}

	spec := w.SpecMap()
	loader, _ := spec["loader"].(string)
	gameVersion, _ := spec["mc_version"].(string)

	addonType := req.Msg.AddonType
	if addonType == v1.AddonType_ADDON_TYPE_UNSPECIFIED {
		// Infer from loader
		lowerLoader := strings.ToLower(loader)
		if lowerLoader == "fabric" || lowerLoader == "forge" || lowerLoader == "neoforge" || lowerLoader == "quilt" {
			addonType = v1.AddonType_ADDON_TYPE_MOD
		} else {
			addonType = v1.AddonType_ADDON_TYPE_PLUGIN
		}
	}

	var downloadURL string
	var filename string

	if req.Msg.DownloadUrl != "" {
		downloadURL = req.Msg.DownloadUrl
		filename = req.Msg.Filename
		if filename == "" {
			parsed, err := url.Parse(downloadURL)
			if err == nil {
				filename = filepath.Base(parsed.Path)
			}
			if filename == "" || filename == "." || filename == "/" {
				filename = "addon-" + uuid.NewString()[:8] + ".jar"
			}
		}
	} else if req.Msg.ModrinthProjectId != "" {
		dURL, fName, err := s.modrinthClient.ResolveDownloadURL(ctx, req.Msg.ModrinthProjectId, req.Msg.ModrinthVersionId, loader, gameVersion)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("resolve modrinth addon: %w", err))
		}
		downloadURL = dURL
		filename = fName
		if req.Msg.Filename != "" {
			filename = req.Msg.Filename
		}
	} else {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("either download_url or modrinth_project_id is required"))
	}

	filename = filepath.Base(filename)
	if !strings.HasSuffix(filename, ".jar") {
		filename += ".jar"
	}

	dir := getAddonDir(addonType)
	targetRelPath := filepath.ToSlash(filepath.Join(dir, filename))

	// Fetch file from URL
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("create download request: %w", err))
	}
	httpReq.Header.Set("User-Agent", "carbon-panel/1.0")

	httpResp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("failed to download addon: %w", err))
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode != http.StatusOK {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("download returned HTTP status %d", httpResp.StatusCode))
	}

	// Stream chunks to node agent
	commandID := uuid.NewString()
	waitCh := s.dispatcher.ExpectFileWrite(commandID)
	defer s.dispatcher.CancelFileWrite(commandID)

	buf := make([]byte, nodeagent.MaxChunkSize)
	lr := io.LimitReader(httpResp.Body, maxAddonDownloadSize)
	var totalBytes int64

	for {
		n, rErr := lr.Read(buf)
		if n > 0 {
			totalBytes += int64(n)
			isLast := rErr == io.EOF
			chunkCopy := make([]byte, n)
			copy(chunkCopy, buf[:n])

			ok := s.dispatcher.DispatchContext(ctx, w.NodeID, &v1.ControlMessage{
				Payload: &v1.ControlMessage_FileWrite{
					FileWrite: &v1.ControlWriteFileChunk{
						CommandId:  commandID,
						WorkloadId: w.ID,
						Path:       targetRelPath,
						Chunk:      chunkCopy,
						IsLast:     isLast,
						Mode:       0644,
					},
				},
			})
			if !ok {
				return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch write chunk to node agent"))
			}

			if isLast {
				break
			}
		}
		if rErr != nil {
			if errors.Is(rErr, io.EOF) {
				// Conclude file write on EOF
				ok := s.dispatcher.DispatchContext(ctx, w.NodeID, &v1.ControlMessage{
					Payload: &v1.ControlMessage_FileWrite{
						FileWrite: &v1.ControlWriteFileChunk{
							CommandId:  commandID,
							WorkloadId: w.ID,
							Path:       targetRelPath,
							Chunk:      []byte{},
							IsLast:     true,
							Mode:       0644,
						},
					},
				})
				if !ok {
					return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch final chunk to node agent"))
				}
				break
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("read download stream: %w", rErr))
		}
	}

	// Wait for node agent write confirmation
	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(30 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for node agent to write addon file"))
	case res := <-waitCh:
		if !res.Success {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("write addon failed on node: %s", res.Error))
		}
	}

	baseName := strings.TrimSuffix(filename, ".jar")
	return connect.NewResponse(&v1.InstallAddonResponse{
		Addon: &v1.InstalledAddon{
			Filename:       filename,
			Name:           baseName,
			AddonType:      addonType,
			Enabled:        true,
			SizeBytes:      totalBytes,
			ModifiedAtUnix: time.Now().Unix(),
		},
	}), nil
}

// SearchAddons searches Modrinth marketplace for plugins/mods.
func (s *AddonService) SearchAddons(ctx context.Context, req *connect.Request[v1.SearchAddonsRequest]) (*connect.Response[v1.SearchAddonsResponse], error) {
	res, err := s.modrinthClient.Search(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("search addons: %w", err))
	}
	return connect.NewResponse(res), nil
}

// GetAddonDetails fetches detailed project info and versions.
func (s *AddonService) GetAddonDetails(ctx context.Context, req *connect.Request[v1.GetAddonDetailsRequest]) (*connect.Response[v1.GetAddonDetailsResponse], error) {
	if strings.TrimSpace(req.Msg.ProjectIdOrSlug) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("project_id_or_slug is required"))
	}

	details, err := s.modrinthClient.GetProject(ctx, req.Msg.ProjectIdOrSlug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("get addon details: %w", err))
	}

	versions, err := s.modrinthClient.GetVersions(ctx, req.Msg.ProjectIdOrSlug, req.Msg.Loader, req.Msg.GameVersion)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get addon versions: %w", err))
	}

	return connect.NewResponse(&v1.GetAddonDetailsResponse{
		Details:  details,
		Versions: versions,
	}), nil
}
