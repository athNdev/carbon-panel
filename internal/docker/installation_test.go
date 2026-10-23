// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadOrCreateInstallationIDCreatesAndStaysStable(t *testing.T) {
	dir := t.TempDir()

	first, err := LoadOrCreateInstallationID(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateInstallationID() error = %v", err)
	}
	if len(first) < 16 {
		t.Fatalf("installation id %q is implausibly short", first)
	}

	path := filepath.Join(dir, installationIDFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("installation id file missing: %v", err)
	}
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Errorf("installation id file permissions = %o, want 600", perm)
	}

	second, err := LoadOrCreateInstallationID(dir)
	if err != nil {
		t.Fatalf("second LoadOrCreateInstallationID() error = %v", err)
	}
	if second != first {
		t.Errorf("installation id changed across restarts: %q -> %q", first, second)
	}
}

func TestLoadOrCreateInstallationIDRejectsMalformedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, installationIDFileName)
	if err := os.WriteFile(path, []byte("definitely not an identity"), 0o600); err != nil {
		t.Fatalf("write malformed id: %v", err)
	}

	if _, err := LoadOrCreateInstallationID(dir); err == nil {
		t.Fatal("expected a malformed identity file to fail closed instead of minting a new identity")
	}
}

func TestLoadOrCreateInstallationIDEmptyDataDir(t *testing.T) {
	if _, err := LoadOrCreateInstallationID("   "); err == nil {
		t.Fatal("expected an empty data directory to be rejected")
	}
}

func TestClassifyOwnership(t *testing.T) {
	tests := []struct {
		name      string
		container string
		ours      string
		want      containerOwnership
	}{
		{name: "matching identity is owned", container: "abc", ours: "abc", want: ownershipOwned},
		{name: "different identity is foreign", container: "other", ours: "abc", want: ownershipForeign},
		{name: "unlabeled container is legacy", container: "", ours: "abc", want: ownershipLegacy},
		{name: "labeled container with no local identity is foreign", container: "abc", ours: "", want: ownershipForeign},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyOwnership(tt.container, tt.ours); got != tt.want {
				t.Errorf("classifyOwnership(%q, %q) = %v, want %v", tt.container, tt.ours, got, tt.want)
			}
		})
	}
}

func TestApplyInstallationLabel(t *testing.T) {
	client := &Client{config: ClientConfig{InstallationID: "installation-a"}}
	labels := map[string]string{}
	client.applyInstallationLabel(labels)
	if got := labels[LabelInstallation]; got != "installation-a" {
		t.Errorf("label = %q, want %q", got, "installation-a")
	}

	// Without a configured identity the label is omitted rather than written empty.
	labels = map[string]string{}
	(&Client{}).applyInstallationLabel(labels)
	if _, ok := labels[LabelInstallation]; ok {
		t.Errorf("unexpected installation label %q for an unconfigured client", labels[LabelInstallation])
	}
}

func TestValidateConflictOwnership(t *testing.T) {
	client := &Client{config: ClientConfig{InstallationID: "ours"}}
	desired := map[string]string{"carbon-panel.server.id": "srv-1"}

	tests := []struct {
		name     string
		existing map[string]string
		wantErr  bool
	}{
		{
			name:     "same installation and entity is adoptable",
			existing: map[string]string{LabelInstallation: "ours", "carbon-panel.server.id": "srv-1"},
		},
		{
			name:     "legacy unlabeled container keeps existing behavior",
			existing: map[string]string{},
		},
		{
			name:     "foreign installation is rejected",
			existing: map[string]string{LabelInstallation: "theirs", "carbon-panel.server.id": "srv-1"},
			wantErr:  true,
		},
		{
			name:     "foreign server id is rejected",
			existing: map[string]string{"carbon-panel.server.id": "srv-other"},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.validateConflictOwnership("container-1", tt.existing, desired)
			if tt.wantErr && err == nil {
				t.Fatal("expected conflict ownership validation to reject the container")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected ownership rejection: %v", err)
			}
		})
	}
}
