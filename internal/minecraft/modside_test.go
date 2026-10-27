// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package minecraft

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeJarWithEntry(t *testing.T, path, entryName, content string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create jar: %v", err)
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	w, err := zw.Create(entryName)
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
}

// TestInspectModSideFabric locks the ME-04 client-only detection for Fabric
// jars, whose `environment` field decides the side.
func TestInspectModSideFabric(t *testing.T) {
	cases := []struct {
		name string
		json string
		want ModSide
	}{
		{"client only", `{"environment": "client"}`, SideClient},
		{"server only", `{"environment": "server"}`, SideServer},
		{"wildcard is both", `{"environment": "*"}`, SideBoth},
		{"empty is both", `{}`, SideBoth},
		{"missing environment is both", `{"id": "x", "version": "1.0"}`, SideBoth},
		{"case insensitive", `{"environment": "CLIENT"}`, SideClient},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			jar := filepath.Join(dir, "mod.jar")
			writeJarWithEntry(t, jar, "fabric.mod.json", c.json)

			got, err := InspectModSide(jar)
			if err != nil {
				t.Fatalf("InspectModSide: %v", err)
			}
			if got != c.want {
				t.Errorf("InspectModSide(%s) = %q, want %q", c.json, got, c.want)
			}
		})
	}
}

// TestInspectModSideQuilt checks Quilt's nested quilt_loader.environment.
func TestInspectModSideQuilt(t *testing.T) {
	dir := t.TempDir()
	jar := filepath.Join(dir, "quilt.jar")
	writeJarWithEntry(t, jar, "quilt.mod.json", `{"quilt_loader": {"environment": "client"}}`)

	got, err := InspectModSide(jar)
	if err != nil {
		t.Fatalf("InspectModSide: %v", err)
	}
	if got != SideClient {
		t.Errorf("quilt client = %q, want %q", got, SideClient)
	}
}

// TestIsClientOnly is the convenience predicate used by upload guards.
func TestIsClientOnly(t *testing.T) {
	dir := t.TempDir()

	client := filepath.Join(dir, "client.jar")
	writeJarWithEntry(t, client, "fabric.mod.json", `{"environment": "client"}`)
	if ok, err := IsClientOnly(client); err != nil || !ok {
		t.Errorf("IsClientOnly(client) = %v, %v; want true, nil", ok, err)
	}

	server := filepath.Join(dir, "server.jar")
	writeJarWithEntry(t, server, "fabric.mod.json", `{"environment": "server"}`)
	if ok, err := IsClientOnly(server); err != nil || ok {
		t.Errorf("IsClientOnly(server) = %v, %v; want false, nil", ok, err)
	}
}

// TestInspectModSideUnknownForForge: Forge/NeoForge jars declare nothing
// reliable, so they must be "unknown" rather than a wrong guess.
func TestInspectModSideUnknownForForge(t *testing.T) {
	dir := t.TempDir()
	jar := filepath.Join(dir, "forge.jar")
	writeJarWithEntry(t, jar, "META-INF/mods.toml", "modLoader=\"javafml\"")

	got, err := InspectModSide(jar)
	if err != nil {
		t.Fatalf("InspectModSide: %v", err)
	}
	if got != SideUnknown {
		t.Errorf("forge jar = %q, want %q", got, SideUnknown)
	}
}

// TestInspectModSideMalformedJSON: a corrupt descriptor must not be trusted as
// "both"; it is an error so the caller can reject the upload.
func TestInspectModSideMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	jar := filepath.Join(dir, "bad.jar")
	writeJarWithEntry(t, jar, "fabric.mod.json", "{not json")

	if _, err := InspectModSide(jar); err == nil {
		t.Fatal("expected an error for malformed fabric.mod.json")
	}
}

// TestInspectModSideNotAZip guards the error path.
func TestInspectModSideNotAZip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notajar.jar")
	if err := os.WriteFile(path, []byte("plain text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectModSide(path); err == nil {
		t.Fatal("expected an error for a non-zip file")
	}
}
