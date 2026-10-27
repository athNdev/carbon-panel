// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"path/filepath"
	"testing"

	"github.com/athNdev/carbon-panel/internal/nodestorage"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// TestBindSourceMapsNodeStorageLayout pins the ADR 0005 compute-plane half:
// a node that declares a storage root binds its own layout, and sources that
// cannot be represented there are reported as unmappable instead of binding
// a controller-local path into a remote container.
func TestBindSourceMapsNodeStorageLayout(t *testing.T) {
	t.Setenv("CARBONPANEL_HOST_DATA_PATH", "")
	base := t.TempDir()
	dataRoot := filepath.Join(base, "servers", "mc")
	sub := filepath.Join(dataRoot, "plugins", "x.jar")
	outside := filepath.Join(base, "backups", "b.zip")

	legacy := &Client{}
	if got, ok := legacy.bindSource(dataRoot, sub); !ok || got != sub {
		t.Fatalf("legacy bindSource = %q, %v; want unchanged", got, ok)
	}

	node := &Client{nodeStorage: nodestorage.NodeInfo{Name: "r1", StorageRoot: filepath.Join(base, "shared")}}
	want := filepath.Join(base, "shared", "mc", "plugins", "x.jar")
	if got, ok := node.bindSource(dataRoot, sub); !ok || got != want {
		t.Fatalf("node bindSource(sub) = %q, %v; want %q", got, ok, want)
	}
	wantRoot := filepath.Join(base, "shared", "mc")
	if got, ok := node.bindSource(dataRoot, dataRoot); !ok || got != wantRoot {
		t.Fatalf("node bindSource(root) = %q, %v; want %q", got, ok, wantRoot)
	}
	if got, ok := node.bindSource(dataRoot, outside); ok {
		t.Fatalf("out-of-root bind must not map onto a shared layout, got %q", got)
	}

	bare := &Client{nodeStorage: nodestorage.NodeInfo{Name: "r2"}}
	if got, ok := bare.bindSource(dataRoot, sub); !ok || got != sub {
		t.Fatalf("bare bindSource = %q, %v; want unchanged (identical-path contract)", got, ok)
	}
}

// TestModuleVolumesToMountsMapsNodeStorageLayout pins the module mount
// translation: in-root binds map to the node layout, unmappable binds are
// skipped, and identical-path daemons are unchanged.
func TestModuleVolumesToMountsMapsNodeStorageLayout(t *testing.T) {
	t.Setenv("CARBONPANEL_HOST_DATA_PATH", "")
	base := t.TempDir()
	dataRoot := filepath.Join(base, "servers", "mc")
	sub := filepath.Join(dataRoot, "plugins")
	outside := filepath.Join(base, "backups")

	vols := []ModuleVolumeMount{
		{Source: sub, Target: "/plugins"},
		{Source: outside, Target: "/backups"},
	}

	node := &Client{log: logger.New(), nodeStorage: nodestorage.NodeInfo{Name: "r1", StorageRoot: filepath.Join(base, "shared")}}
	mounts := node.moduleVolumesToMounts(vols, dataRoot, dataRoot, outside)
	if len(mounts) != 1 {
		t.Fatalf("want 1 mount (unmappable bind skipped), got %d", len(mounts))
	}
	if want := filepath.Join(base, "shared", "mc", "plugins"); mounts[0].Source != want {
		t.Fatalf("mount source = %q, want %q", mounts[0].Source, want)
	}

	legacy := &Client{log: logger.New()}
	legacyMounts := legacy.moduleVolumesToMounts(vols, dataRoot, dataRoot, outside)
	if len(legacyMounts) != 2 {
		t.Fatalf("want 2 mounts for identical-path daemon, got %d", len(legacyMounts))
	}
	if legacyMounts[0].Source != sub {
		t.Fatalf("legacy mount source = %q, want %q", legacyMounts[0].Source, sub)
	}
}
