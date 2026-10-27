// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package nodestorage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testAccessor(t *testing.T) (Accessor, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "srv-data")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	a, err := NewLocal(root)
	t.Cleanup(func() { _ = a.Close() })
	if err != nil {
		t.Fatal(err)
	}
	return a, root
}

func TestLocalFileRoundTrip(t *testing.T) {
	a, _ := testAccessor(t)

	if err := a.WriteFile("world/level.dat", []byte("level")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := a.ReadFile("world/level.dat")
	if err != nil || string(got) != "level" {
		t.Fatalf("ReadFile = %q, %v", got, err)
	}
	if !strings.HasSuffix(a.Root(), "srv-data") {
		t.Fatalf("Root() = %q", a.Root())
	}

	entries, err := a.ReadDir("world")
	if err != nil || len(entries) != 1 || entries[0].Name() != "level.dat" {
		t.Fatalf("ReadDir = %v, %v", entries, err)
	}

	if err := a.MkdirAll("logs"); err != nil {
		t.Fatal(err)
	}
	if err := a.WriteFile("logs/latest.log", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := a.Rename("logs/latest.log", "logs/old.log"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := a.Remove("logs/old.log"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := a.Stat("logs/old.log"); !os.IsNotExist(err) {
		t.Fatalf("removed file must be gone, stat err = %v", err)
	}
	if err := a.RemoveAll("logs"); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
}

func TestLocalRejectsEscapes(t *testing.T) {
	a, _ := testAccessor(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{"../evil", "..", "/etc/passwd", "world/../../evil", strings.Repeat("../", 3) + "evil"} {
		if err := a.WriteFile(rel, []byte("x")); err == nil || !IsEscape(err) {
			t.Errorf("WriteFile(%q) err = %v, want escape rejection", rel, err)
		}
		if _, err := a.ReadFile(rel); err == nil || !IsEscape(err) {
			t.Errorf("ReadFile(%q) err = %v, want escape rejection", rel, err)
		}
	}
}

func TestForNodeRefusesRemoteWithoutAdapter(t *testing.T) {
	root := filepath.Join(t.TempDir(), "srv-data")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}

	localAcc, err := ForNode(NodeInfo{Name: "local", IsLocal: true}, root)
	defer func() { _ = localAcc.Close() }()
	if err != nil || localAcc == nil {
		t.Fatalf("local node must get an accessor: %v", err)
	}

	if _, err := ForNode(NodeInfo{Name: "remote-1", IsLocal: false}, root); err == nil || !IsUnavailable(err) {
		t.Fatalf("remote node must be refused with ErrUnavailable, got %v", err)
	}
}

func TestMapDataPath(t *testing.T) {
	tests := []struct {
		name string
		node NodeInfo
		want string
		ref  bool
	}{
		{
			name: "local keeps the data root",
			node: NodeInfo{Name: "local", IsLocal: true, StorageRoot: "/mnt/shared"},
			want: filepath.Join("var", "data", "servers", "mc"),
		},
		{
			name: "remote shared storage defaults to the node-side root",
			node: NodeInfo{Name: "r1", StorageRoot: filepath.Join("mnt", "shared")},
			want: filepath.Join("mnt", "shared", "mc"),
		},
		{
			name: "controller mount wins when set",
			node: NodeInfo{Name: "r2", StorageRoot: "/srv/nfs", ControllerStorageRoot: filepath.Join("mnt", "nfs")},
			want: filepath.Join("mnt", "nfs", "mc"),
		},
		{
			name: "controller mount alone is enough to serve files",
			node: NodeInfo{Name: "r3", ControllerStorageRoot: filepath.Join("mnt", "nfs")},
			want: filepath.Join("mnt", "nfs", "mc"),
		},
		{
			name: "remote without roots is refused",
			node: NodeInfo{Name: "r4"},
			ref:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MapDataPath(tt.node, filepath.Join("var", "data", "servers", "mc"))
			if tt.ref {
				if err == nil || !IsUnavailable(err) {
					t.Fatalf("want ErrUnavailable, got %q, %v", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("MapDataPath = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestForNodeMapsRemoteSharedStorage(t *testing.T) {
	base := t.TempDir()
	mount := filepath.Join(base, "shared")
	if err := os.MkdirAll(mount, 0o755); err != nil {
		t.Fatal(err)
	}
	node := NodeInfo{Name: "r1", StorageRoot: mount}
	dataRoot := filepath.Join("var", "data", "servers", "mc")

	acc, err := ForNode(node, dataRoot)
	if err != nil {
		t.Fatalf("ForNode: %v", err)
	}
	defer func() { _ = acc.Close() }()

	wantRoot := filepath.Join(mount, "mc")
	if acc.Root() != wantRoot {
		t.Fatalf("Root() = %q, want %q", acc.Root(), wantRoot)
	}
	if err := acc.WriteFile("server.properties", []byte("p")); err != nil {
		t.Fatalf("WriteFile through mapped root: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(wantRoot, "server.properties")); err != nil || string(got) != "p" {
		t.Fatalf("file must land in the mapped root: %q, %v", got, err)
	}
}

func TestEnsureDataRootRequiresExistingMount(t *testing.T) {
	base := t.TempDir()
	mount := filepath.Join(base, "not-mounted")
	node := NodeInfo{Name: "r1", StorageRoot: mount}

	if _, err := EnsureDataRoot(node, filepath.Join("var", "data", "servers", "mc")); err == nil || !IsUnavailable(err) {
		t.Fatalf("missing mount must be refused with ErrUnavailable, got %v", err)
	}
	if _, err := os.Stat(mount); !os.IsNotExist(err) {
		t.Fatalf("a missing storage root must never be created (hidden mount guard), stat err = %v", err)
	}

	if err := os.MkdirAll(mount, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := EnsureDataRoot(node, filepath.Join("var", "data", "servers", "mc"))
	if err != nil {
		t.Fatalf("EnsureDataRoot with mounted root: %v", err)
	}
	want := filepath.Join(mount, "mc")
	if got != want {
		t.Fatalf("EnsureDataRoot = %q, want %q", got, want)
	}
	if fi, err := os.Stat(want); err != nil || !fi.IsDir() {
		t.Fatalf("server directory must be created under the mount: %v", err)
	}
}

func TestIsLegacyLocalNodeID(t *testing.T) {
	for _, id := range []string{"", "local", "default"} {
		if !IsLegacyLocalNodeID(id) {
			t.Errorf("IsLegacyLocalNodeID(%q) = false", id)
		}
	}
	for _, id := range []string{"node-1", "remote", "LOCAL"} {
		if IsLegacyLocalNodeID(id) {
			t.Errorf("IsLegacyLocalNodeID(%q) = true", id)
		}
	}
}
