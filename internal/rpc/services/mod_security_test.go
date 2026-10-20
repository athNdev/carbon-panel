// SPDX-License-Identifier: MPL-2.0

package services

import (
	"path/filepath"
	"testing"
)

func TestModInstallPathRejectsTraversal(t *testing.T) {
	modsDir := t.TempDir()
	for _, filename := range []string{"..", `..\outside.jar`, "../outside.jar", `nested\mod.jar`, "/tmp/mod.jar", `C:\outside.jar`} {
		t.Run(filename, func(t *testing.T) {
			if path, err := modInstallPath(modsDir, filename); err == nil {
				t.Fatalf("modInstallPath(%q) = %q, want an error", filename, path)
			}
		})
	}
}

func TestModInstallPathKeepsFilenameInModsDirectory(t *testing.T) {
	modsDir := t.TempDir()
	path, err := modInstallPath(modsDir, "lithium-fabric.jar")
	if err != nil {
		t.Fatalf("modInstallPath returned error: %v", err)
	}
	if want := filepath.Join(modsDir, "lithium-fabric.jar"); path != want {
		t.Fatalf("modInstallPath returned %q, want %q", path, want)
	}
}
