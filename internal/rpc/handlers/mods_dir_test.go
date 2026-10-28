// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/packwiz"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

func seedRemoteModServer(t *testing.T, store *storage.Store, id, dataPath, storageRoot string) *storage.Server {
	t.Helper()
	ctx := context.Background()
	if err := store.CreateNode(ctx, &storage.Node{
		ID: id + "-node", Name: id + "-node", Host: "tcp://10.3.0.4:2375", Enabled: true,
		StorageRoot: storageRoot,
	}); err != nil {
		t.Fatal(err)
	}
	server := &storage.Server{
		ID: id, Name: id, NodeID: id + "-node", DataPath: dataPath,
		ModLoader: storage.ModLoaderFabric, MCVersion: "1.20.1",
	}
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveServerConfig(ctx, &storage.ServerConfig{ID: id + "-cfg", ServerID: id}); err != nil {
		t.Fatal(err)
	}
	return server
}

func TestInstalledModsComeFromTheMappedRoot(t *testing.T) {
	store := setupTestStore(t)
	dir := t.TempDir()
	mount := filepath.Join(dir, "mount")
	mappedMods := filepath.Join(mount, "srv-data", "mods")
	controllerMods := filepath.Join(dir, "controller", "srv-data", "mods")
	if err := os.MkdirAll(mappedMods, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(controllerMods, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mappedMods, "from-mount.jar"), []byte("mount"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(controllerMods, "from-controller.jar"), []byte("controller"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := seedRemoteModServer(t, store, "srv-mods", filepath.Join(dir, "controller", "srv-data"), mount)

	installed := installedModJars(context.Background(), store, server)
	if !installed["from-mount.jar"] || installed["from-controller.jar"] {
		t.Fatalf("installed = %v", installed)
	}
}

func TestInstallModWritesTheMappedRoot(t *testing.T) {
	store := setupTestStore(t)
	dir := t.TempDir()
	mount := filepath.Join(dir, "mount")
	if err := os.MkdirAll(mount, 0o755); err != nil {
		t.Fatal(err)
	}
	controller := filepath.Join(dir, "controller", "srv-data", "mods")
	if err := os.MkdirAll(controller, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(controller, "from-controller.jar"), []byte("controller"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := seedRemoteModServer(t, store, "srv-install", filepath.Join(dir, "controller", "srv-data"), mount)

	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("jar-bytes"))
	}))
	t.Cleanup(download.Close)

	body, err := json.Marshal(InstallModsRequest{Items: []InstallModItem{{
		Name: "Example", FileName: "example.jar", DownloadURL: download.URL,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/install", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewModOnlineManager(store, logger.New(), nil, nil).handleInstall(rec, req, server)
	if rec.Code != http.StatusOK {
		t.Fatalf("install status = %d body %s", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(mount, "srv-data", "mods", "example.jar"))
	if err != nil || string(got) != "jar-bytes" {
		t.Fatalf("mapped jar = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(controller, "example.jar")); !os.IsNotExist(err) {
		t.Fatalf("controller jar exists: %v", err)
	}
}

func TestInstallModRefusesAnUnreachableRemoteNode(t *testing.T) {
	store := setupTestStore(t)
	controller := filepath.Join(t.TempDir(), "controller", "srv-data")
	server := seedRemoteModServer(t, store, "srv-bare", controller, "")

	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("jar-bytes"))
	}))
	t.Cleanup(download.Close)

	body, err := json.Marshal(InstallModsRequest{Items: []InstallModItem{{
		Name: "Example", FileName: "example.jar", DownloadURL: download.URL,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/install", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewModOnlineManager(store, logger.New(), nil, nil).handleInstall(rec, req, server)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("install status = %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(controller); !os.IsNotExist(err) {
		t.Fatalf("controller data path %s: %v", controller, err)
	}
}

func TestPackwizBakeWritesTheMappedRoot(t *testing.T) {
	store := setupTestStore(t)
	dir := t.TempDir()
	mount := filepath.Join(dir, "mount")
	if err := os.MkdirAll(mount, 0o755); err != nil {
		t.Fatal(err)
	}
	server := seedRemoteModServer(t, store, "srv-bake", filepath.Join(dir, "controller", "srv-data"), mount)

	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("baked-jar"))
	}))
	t.Cleanup(download.Close)

	mgr := packwiz.NewManager(t.TempDir(), logger.New())
	pack, err := mgr.CreatePack(&packwiz.Pack{
		Name: "Bake", MCVersion: "1.20.1", ModLoader: "fabric",
		Mods: []packwiz.ModItem{{
			Name: "Example", FileName: "example.jar", Side: "server", DownloadURL: download.URL,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(DeployPackRequest{ServerID: server.ID, Mode: "bake"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/deploy", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	NewPackwizHandler(mgr, store, nil, logger.New(), nil, nil, nil).handleDeploy(rec, req, pack.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("bake status = %d body %s", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(mount, "srv-data", "mods", "example.jar"))
	if err != nil || string(got) != "baked-jar" {
		t.Fatalf("mapped jar = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "controller", "srv-data", "mods", "example.jar")); !os.IsNotExist(err) {
		t.Fatalf("controller jar exists: %v", err)
	}
}
