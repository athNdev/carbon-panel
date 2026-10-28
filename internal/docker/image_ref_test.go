// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"strings"
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
)

const sampleDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestCanonicalServerImage(t *testing.T) {
	cases := []struct {
		in      string
		stored  string
		resolve string
	}{
		{in: "java21", stored: "java21", resolve: "itzg/minecraft-server:java21"},
		{in: "itzg/minecraft-server:java21", stored: "java21", resolve: "itzg/minecraft-server:java21"},
		{in: "docker.io/itzg/minecraft-server:java21", stored: "java21", resolve: "itzg/minecraft-server:java21"},
		{
			in:      "itzg/minecraft-bedrock-server:latest",
			stored:  "itzg/minecraft-bedrock-server:latest",
			resolve: "itzg/minecraft-bedrock-server:latest",
		},
		{
			in:      "docker.io/itzg/minecraft-bedrock-server:latest",
			stored:  "itzg/minecraft-bedrock-server:latest",
			resolve: "itzg/minecraft-bedrock-server:latest",
		},
		{
			in:      "itzg/minecraft-server:java21@" + sampleDigest,
			stored:  "itzg/minecraft-server:java21@" + sampleDigest,
			resolve: "itzg/minecraft-server:java21@" + sampleDigest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			stored, err := CanonicalServerImage(tc.in)
			if err != nil {
				t.Fatalf("CanonicalServerImage(%q) error = %v", tc.in, err)
			}
			if stored != tc.stored {
				t.Fatalf("stored = %q, want %q", stored, tc.stored)
			}
			resolved, err := ResolveServerImage(stored)
			if err != nil {
				t.Fatalf("ResolveServerImage(%q) error = %v", stored, err)
			}
			if resolved != tc.resolve {
				t.Fatalf("resolved = %q, want %q", resolved, tc.resolve)
			}
			if strings.Contains(resolved, "itzg/minecraft-server:itzg/") {
				t.Fatalf("resolved %q repeats the repository", resolved)
			}
		})
	}
}

func TestCanonicalServerImageRejectsOtherRepositories(t *testing.T) {
	for _, ref := range []string{
		"nginx:latest",
		"ghcr.io/example/evil:1",
		"library/nginx:latest",
		"itzg/minecraft-server:itzg/minecraft-bedrock-server:latest",
		"itzg/mc-backup:latest",
	} {
		t.Run(ref, func(t *testing.T) {
			if _, err := CanonicalServerImage(ref); err == nil {
				t.Fatalf("CanonicalServerImage(%q) succeeded", ref)
			}
		})
	}
}

func TestServerImageNameUsesTheResolvedReference(t *testing.T) {
	name, err := serverImageName(&models.Server{DockerImage: "itzg/minecraft-bedrock-server:latest"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "itzg/minecraft-bedrock-server:latest" {
		t.Fatalf("serverImageName() = %q", name)
	}
}
