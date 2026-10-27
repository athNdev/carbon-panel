// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package minecraft

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ModSide describes which Minecraft side a mod artifact targets.
type ModSide string

const (
	// SideClient means the mod is client-only and will crash a dedicated server.
	SideClient ModSide = "client"
	// SideServer means the mod targets the server side only.
	SideServer ModSide = "server"
	// SideBoth means the mod runs on both sides (the Fabric default).
	SideBoth ModSide = "both"
	// SideUnknown means the metadata declares nothing reliable (e.g. Forge).
	SideUnknown ModSide = "unknown"
)

// InspectModSide reads a mod jar's metadata to determine whether it targets the
// client, the server, or both. Client-only mods crash dedicated servers at class
// load, so a panel must catch them before installing (ME-04).
//
// Fabric and Quilt declare an explicit environment; Forge and NeoForge have no
// reliable equivalent, so those return SideUnknown rather than guessing.
func InspectModSide(jarPath string) (ModSide, error) {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return SideUnknown, fmt.Errorf("open mod jar: %w", err)
	}
	defer func() { _ = r.Close() }()

	for _, name := range []string{"fabric.mod.json", "quilt.mod.json"} {
		f := findZipEntry(&r.Reader, name)
		if f == nil {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return SideUnknown, fmt.Errorf("read %s: %w", name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return SideUnknown, fmt.Errorf("read %s: %w", name, err)
		}

		var meta struct {
			Environment string `json:"environment"`
			QuiltLoader struct {
				Environment string `json:"environment"`
			} `json:"quilt_loader"`
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			return SideUnknown, fmt.Errorf("parse %s: %w", name, err)
		}

		env := meta.Environment
		if env == "" {
			env = meta.QuiltLoader.Environment
		}
		return sideFromEnvironment(env), nil
	}

	return SideUnknown, nil
}

// IsClientOnly reports whether the jar targets the client exclusively. It
// returns false for unknown metadata; callers that must be strict should check
// InspectModSide directly.
func IsClientOnly(jarPath string) (bool, error) {
	side, err := InspectModSide(jarPath)
	if err != nil {
		return false, err
	}
	return side == SideClient, nil
}

// sideFromEnvironment maps a Fabric/Quilt environment string to a ModSide. An
// empty or "*" environment means both sides.
func sideFromEnvironment(env string) ModSide {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "client":
		return SideClient
	case "server":
		return SideServer
	default:
		return SideBoth
	}
}

func findZipEntry(r *zip.Reader, name string) *zip.File {
	for _, f := range r.File {
		if strings.EqualFold(f.Name, name) {
			return f
		}
	}
	return nil
}
