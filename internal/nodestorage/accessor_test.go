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
