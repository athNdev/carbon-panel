// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package services

import (
	"testing"

	storage "github.com/athNdev/carbon-panel/internal/db"
)

func TestConfigureModrinthModpackPinnedVersion(t *testing.T) {
	config := &storage.ServerConfig{}
	configureModrinthModpack(config, "Jkb29YJU", "1.7.42")
	if got := *config.ModrinthModpack; got != "Jkb29YJU" {
		t.Fatalf("Modrinth project = %q, want project ID", got)
	}
	if got := *config.ModrinthVersion; got != "1.7.42" {
		t.Fatalf("Modrinth version = %q, want pinned version", got)
	}
	if config.ModrinthModpackVersionType != nil {
		t.Fatalf("pinned version must not retain a version type: %q", *config.ModrinthModpackVersionType)
	}
	if got := *config.ModrinthDownloadDependencies; got != "required" {
		t.Fatalf("dependency mode = %q, want required", got)
	}
}

func TestConfigureModrinthModpackLatestClearsPriorPin(t *testing.T) {
	oldPin := "1.7.42"
	oldType := "beta"
	config := &storage.ServerConfig{ModrinthVersion: &oldPin, ModrinthModpackVersionType: &oldType}
	configureModrinthModpack(config, "Jkb29YJU", "latest")
	if got := *config.ModrinthModpack; got != "Jkb29YJU" {
		t.Fatalf("Modrinth project = %q, want project ID", got)
	}
	if config.ModrinthVersion != nil {
		t.Fatalf("latest selection must clear old pinned version: %q", *config.ModrinthVersion)
	}
	if got := *config.ModrinthModpackVersionType; got != "release" {
		t.Fatalf("version type = %q, want release", got)
	}
}

func TestConfigureModrinthModpackEmptyVersionUsesLatestRelease(t *testing.T) {
	config := &storage.ServerConfig{}
	configureModrinthModpack(config, "Jkb29YJU", "")
	if config.ModrinthVersion != nil {
		t.Fatalf("empty version must not set a pinned version: %q", *config.ModrinthVersion)
	}
	if got := *config.ModrinthModpackVersionType; got != "release" {
		t.Fatalf("version type = %q, want release", got)
	}
}
