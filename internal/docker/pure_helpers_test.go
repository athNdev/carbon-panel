// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package docker

import (
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
	"github.com/moby/moby/api/types/container"
)

func TestParseDNSAddrs(t *testing.T) {
	addrs := parseDNSAddrs([]string{"8.8.8.8", "1.1.1.1"})
	if len(addrs) != 2 || addrs[0].String() != "8.8.8.8" || addrs[1].String() != "1.1.1.1" {
		t.Errorf("parseDNSAddrs = %v", addrs)
	}
	if got := parseDNSAddrs(nil); len(got) != 0 {
		t.Errorf("parseDNSAddrs(nil) = %v, want empty", got)
	}
	// Invalid entries are skipped, valid ones survive.
	addrs = parseDNSAddrs([]string{"not-an-ip", "9.9.9.9", " 8.8.4.4 "})
	if len(addrs) != 2 || addrs[0].String() != "9.9.9.9" || addrs[1].String() != "8.8.4.4" {
		t.Errorf("parseDNSAddrs with invalid = %v", addrs)
	}
	// IPv6 passes through.
	addrs = parseDNSAddrs([]string{"2001:4860:4860::8888"})
	if len(addrs) != 1 || addrs[0].String() != "2001:4860:4860::8888" {
		t.Errorf("parseDNSAddrs v6 = %v", addrs)
	}
}

func TestTranslateToHostPath(t *testing.T) {
	t.Setenv("CARBONPANEL_HOST_DATA_PATH", "")
	if got := TranslateToHostPath("/data/world"); got != "/data/world" {
		t.Errorf("no env = %q", got)
	}

	t.Setenv("CARBONPANEL_HOST_DATA_PATH", "/host/data")
	t.Setenv("CARBONPANEL_DATA_DIR", "/app/data")
	if got := TranslateToHostPath("/app/data/world"); got != "/host/data/world" {
		t.Errorf("translated = %q", got)
	}
	// Outside the container data dir: returned as-is.
	if got := TranslateToHostPath("/etc/passwd"); got != "/etc/passwd" {
		t.Errorf("outside = %q", got)
	}
}

func TestExtractCurseForgeSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"https://www.curseforge.com/minecraft/modpacks/all-the-mods-9", "all-the-mods-9"},
		{"https://www.curseforge.com/minecraft/mc-mods/sodium", "sodium"},
		{"just-a-slug", "just-a-slug"},
		{"https://example.com/not-a-modpack", ""},
	}
	for _, tc := range cases {
		if got := extractCurseForgeSlug(tc.in); got != tc.want {
			t.Errorf("slug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExtractCurseForgeFileID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"https://www.curseforge.com/minecraft/modpacks/all-the-mods-9/files/1234567", "1234567"},
		{"no-file-here", ""},
	}
	for _, tc := range cases {
		if got := extractCurseForgeFileID(tc.in); got != tc.want {
			t.Errorf("fileID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestInspectToSummary(t *testing.T) {
	summary := inspectToSummary(container.InspectResponse{
		ID:    "abc123",
		Name:  "/carbon-panel-server-x",
		Image: "itzg/minecraft-server:stable",
		Config: &container.Config{
			Image:  "itzg/minecraft-server:stable",
			Labels: map[string]string{"carbon-panel.server.id": "srv-1"},
		},
		State: &container.State{Status: "running"},
	})
	if summary.ID != "abc123" || summary.Image != "itzg/minecraft-server:stable" {
		t.Errorf("summary = %+v", summary)
	}
	if len(summary.Names) != 1 || summary.Names[0] != "/carbon-panel-server-x" {
		t.Errorf("names = %v", summary.Names)
	}
	if string(summary.State) != "running" || summary.Status != "running" {
		t.Errorf("state/status = %q/%q", summary.State, summary.Status)
	}
	if summary.Labels["carbon-panel.server.id"] != "srv-1" {
		t.Errorf("labels = %v", summary.Labels)
	}

	// Nil config/state must not panic and yields zero values.
	bare := inspectToSummary(container.InspectResponse{ID: "x"})
	if bare.ID != "x" || len(bare.Names) != 0 {
		t.Errorf("bare summary = %+v", bare)
	}
}

func TestMapContainerState(t *testing.T) {
	cases := []struct {
		state *container.State
		want  models.ServerStatus
	}{
		{nil, models.StatusError},
		{&container.State{Status: "running"}, models.StatusRunning},
		{&container.State{Status: "running", Health: &container.Health{Status: "healthy"}}, models.StatusRunning},
		{&container.State{Status: "running", Health: &container.Health{Status: "starting"}}, models.StatusStarting},
		{&container.State{Status: "running", Health: &container.Health{Status: "unhealthy"}}, models.StatusUnhealthy},
		{&container.State{Status: "restarting"}, models.StatusStarting},
		{&container.State{Status: "exited"}, models.StatusStopped},
		{&container.State{Status: "dead"}, models.StatusStopped},
		{&container.State{Status: "created"}, models.StatusStopped},
		{&container.State{Status: "paused"}, models.StatusPaused},
		{&container.State{Status: "weird"}, models.StatusError},
	}
	for _, tc := range cases {
		if got := mapContainerState(tc.state); got != tc.want {
			t.Errorf("mapContainerState(%+v) = %q, want %q", tc.state, got, tc.want)
		}
	}
}
