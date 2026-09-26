package svc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/modrinth"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/stretchr/testify/require"
)

func TestAddonService_ModrinthSearchAndDetails(t *testing.T) {
	t.Parallel()

	// Mock Modrinth API server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			resp := map[string]any{
				"hits": []map[string]any{
					{
						"project_id":     "viaversion-id",
						"project_type":   "mod",
						"slug":           "viaversion",
						"author":         "ViaVersionTeam",
						"title":          "ViaVersion",
						"description":    "Allow newer client versions to connect to older server versions",
						"categories":     []string{"paper", "velocity"},
						"downloads":      15000000,
						"latest_version": "5.2.1",
						"date_modified":  "2026-03-01T12:00:00Z",
					},
				},
				"offset":     0,
				"limit":      10,
				"total_hits": 1,
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/project/viaversion":
			resp := map[string]any{
				"id":            "viaversion-id",
				"slug":          "viaversion",
				"project_type":  "mod",
				"title":         "ViaVersion",
				"description":   "Allow newer client versions to connect to older server versions",
				"categories":    []string{"paper", "velocity"},
				"downloads":     15000000,
				"loaders":       []string{"paper", "velocity", "purpur"},
				"game_versions": []string{"1.21.4", "1.20.6"},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/project/viaversion/version":
			resp := []map[string]any{
				{
					"id":             "ver_5_2_1",
					"project_id":     "viaversion-id",
					"name":           "ViaVersion 5.2.1",
					"version_number": "5.2.1",
					"game_versions":  []string{"1.21.4"},
					"loaders":        []string{"paper", "velocity"},
					"files": []map[string]any{
						{
							"url":      "https://cdn.modrinth.com/data/viaversion/5.2.1/ViaVersion-5.2.1.jar",
							"filename": "ViaVersion-5.2.1.jar",
							"primary":  true,
							"size":     1048576,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	bundle := testBundle(t)
	mClient := modrinth.NewClient(ts.Client())
	mClient.SetBaseURL(ts.URL)
	addonSvc := NewAddonService(bundle.Addon.deps, bundle.Dispatcher, mClient, ts.Client())
	ctx := orgCtx()

	// 1. Search addons
	searchResp, err := addonSvc.SearchAddons(ctx, connect.NewRequest(&v1.SearchAddonsRequest{
		Query:  "viaversion",
		Loader: "paper",
	}))
	require.NoError(t, err)
	require.Len(t, searchResp.Msg.Hits, 1)
	hit := searchResp.Msg.Hits[0]
	require.Equal(t, "viaversion", hit.Slug)
	require.Equal(t, "ViaVersion", hit.Title)
	require.Equal(t, int64(15000000), hit.Downloads)

	// 2. Get details
	detResp, err := addonSvc.GetAddonDetails(ctx, connect.NewRequest(&v1.GetAddonDetailsRequest{
		ProjectIdOrSlug: "viaversion",
	}))
	require.NoError(t, err)
	require.Equal(t, "ViaVersion", detResp.Msg.Details.Title)
	require.Len(t, detResp.Msg.Versions, 1)
	require.Equal(t, "ver_5_2_1", detResp.Msg.Versions[0].Id)
	require.Equal(t, "ViaVersion-5.2.1.jar", detResp.Msg.Versions[0].Files[0].Filename)
}

func TestAddonService_ListToggleUninstall(t *testing.T) {
	t.Parallel()
	bundle := testBundle(t)
	ctx := orgCtx()

	// 1. Join a test node
	tok, err := bundle.Node.CreateJoinToken(ctx, connect.NewRequest(&v1.CreateJoinTokenRequest{
		Name:       "addon-node-token",
		TtlSeconds: 3600,
	}))
	require.NoError(t, err)

	joined, err := bundle.Agent.JoinNode(context.Background(), connect.NewRequest(&v1.JoinNodeRequest{
		Token: tok.Msg.Secret,
		Capacity: &v1.NodeCapacity{
			Vcpu: 4, RamMb: 8192, DiskGb: 100,
		},
		Hostname:     "addon-test-node",
		AgentVersion: "0.1.0",
	}))
	require.NoError(t, err)
	nodeID := joined.Msg.Identity.NodeId

	sess := bundle.Dispatcher.Register(nodeID)
	defer bundle.Dispatcher.Unregister(nodeID)

	// 2. Create Workload
	wResp, err := bundle.Workload.CreateWorkload(ctx, connect.NewRequest(&v1.CreateWorkloadRequest{
		NodeId: nodeID,
		Name:   "addon-workload",
	}))
	require.NoError(t, err)
	workloadID := wResp.Msg.Workload.Id

	// 3. Node simulator goroutine
	go func() {
		for msg := range sess.NextMessage() {
			if fl := msg.GetFileList(); fl != nil {
				bundle.Dispatcher.ResolveFileList(&v1.AgentFileListResult{
					CommandId: fl.CommandId,
					Success:   true,
					Files: []*v1.FileInfo{
						{
							Name:           "ViaVersion.jar",
							Path:           "plugins/ViaVersion.jar",
							IsDir:          false,
							Size:           1048576,
							ModifiedAtUnix: 1711000000,
						},
						{
							Name:           "Geyser-Spigot.jar.disabled",
							Path:           "plugins/Geyser-Spigot.jar.disabled",
							IsDir:          false,
							Size:           2097152,
							ModifiedAtUnix: 1711000000,
						},
					},
				})
			} else if fr := msg.GetFileRename(); fr != nil {
				bundle.Dispatcher.ResolveFileRename(&v1.AgentFileRenameResult{
					CommandId: fr.CommandId,
					Success:   true,
				})
			} else if fd := msg.GetFileDelete(); fd != nil {
				bundle.Dispatcher.ResolveFileDelete(&v1.AgentDeleteFileResult{
					CommandId: fd.CommandId,
					Success:   true,
				})
			}
		}
	}()

	// 4. List addons
	listResp, err := bundle.Addon.ListWorkloadAddons(ctx, connect.NewRequest(&v1.ListWorkloadAddonsRequest{
		WorkloadId: workloadID,
		AddonType:  v1.AddonType_ADDON_TYPE_PLUGIN,
	}))
	require.NoError(t, err)
	require.Len(t, listResp.Msg.Addons, 2)

	via := listResp.Msg.Addons[0]
	require.Equal(t, "ViaVersion.jar", via.Filename)
	require.Equal(t, "ViaVersion", via.Name)
	require.True(t, via.Enabled)

	geyser := listResp.Msg.Addons[1]
	require.Equal(t, "Geyser-Spigot.jar.disabled", geyser.Filename)
	require.Equal(t, "Geyser-Spigot", geyser.Name)
	require.False(t, geyser.Enabled)

	// 5. Toggle addon (disable ViaVersion)
	togResp, err := bundle.Addon.ToggleAddon(ctx, connect.NewRequest(&v1.ToggleAddonRequest{
		WorkloadId: workloadID,
		Filename:   "ViaVersion.jar",
		AddonType:  v1.AddonType_ADDON_TYPE_PLUGIN,
		Enable:     false,
	}))
	require.NoError(t, err)
	require.False(t, togResp.Msg.Addon.Enabled)
	require.Equal(t, "ViaVersion.jar.disabled", togResp.Msg.Addon.Filename)
	require.Equal(t, "ViaVersion", togResp.Msg.Addon.Name)

	// 6. Uninstall addon (Geyser)
	_, err = bundle.Addon.UninstallAddon(ctx, connect.NewRequest(&v1.UninstallAddonRequest{
		WorkloadId: workloadID,
		Filename:   "Geyser-Spigot.jar.disabled",
		AddonType:  v1.AddonType_ADDON_TYPE_PLUGIN,
	}))
	require.NoError(t, err)

	// 7. Test FileService.RenameFile directly
	renResp, err := bundle.File.RenameFile(ctx, connect.NewRequest(&v1.RenameFileRequest{
		WorkloadId: workloadID,
		OldPath:    "server.properties",
		NewPath:    "server.properties.old",
	}))
	require.NoError(t, err)
	require.NotNil(t, renResp.Msg)
}
