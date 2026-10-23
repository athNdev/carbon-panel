// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package testharness

import (
	"path/filepath"
	"testing"
	"time"
)

func TestManifestRecordsInstallNodeAndResourceOwnership(t *testing.T) {
	m := &Manifest{Version: ManifestVersion, RunID: "run-1", InstallID: "install-1", NodeID: "node-1", CreatedAt: time.Now(), Daemons: []Daemon{{Alias: "endpoint-a", ID: "daemon-a", DockerRootDir: "/docker-a"}, {Alias: "endpoint-b", ID: "daemon-b", DockerRootDir: "/docker-b"}}}
	labels := m.OwnershipLabels()
	if err := m.AddResource(OwnedResource{DaemonAlias: "endpoint-a", Kind: "container", ID: "container-id", Labels: labels}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "run.json")
	if err := WriteManifest(path, m); err != nil {
		t.Fatal(err)
	}
	got, err := ReadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.InstallID != m.InstallID || got.NodeID != m.NodeID || len(got.Resources) != 1 || got.Resources[0].ID != "container-id" {
		t.Fatalf("round-trip manifest = %#v", got)
	}
	if err := WriteManifest(path, m); err == nil {
		t.Fatal("WriteManifest overwrote an existing run manifest")
	}
}

func TestManifestRejectsUnownedAndUnsupportedResources(t *testing.T) {
	m := &Manifest{Version: ManifestVersion, RunID: "run-1", InstallID: "install-1", NodeID: "node-1"}
	if err := m.AddResource(OwnedResource{DaemonAlias: "endpoint-a", Kind: "container", ID: "container-id", Labels: map[string]string{LabelRun: "other-run", LabelInstall: "install-1", LabelNode: "node-1"}}); err == nil {
		t.Fatal("resource with another run label accepted")
	}
	if err := m.AddResource(OwnedResource{DaemonAlias: "endpoint-a", Kind: "image", ID: "image-id", Labels: m.OwnershipLabels()}); err == nil {
		t.Fatal("resource kind without exact inspect/remove semantics accepted")
	}
}
