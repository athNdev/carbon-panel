// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package minecraft

import (
	"path/filepath"
	"testing"
)

func writeMeta(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pack.zip")
	writeZipWithEntries(t, path, map[string]string{"pack.mcmeta": content})
	return path
}

func TestReadPackFormat(t *testing.T) {
	cases := []struct {
		name    string
		meta    string
		wantMin float64
		wantMax float64
	}{
		{"legacy integer", `{"pack":{"pack_format":48}}`, 48, 48},
		{"range object", `{"pack":{"pack_format":{"min_inclusive":41,"max_inclusive":48}}}`, 41, 48},
		{"supported_formats pair", `{"pack":{"pack_format":48,"supported_formats":{"min_inclusive":26,"max_inclusive":48}}}`, 26, 48},
		{"min/max format (1.21.9+)", `{"pack":{"min_format":88,"max_format":94.1}}`, 88, 94.1},
		{"no format declared", `{"pack":{"description":"x"}}`, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReadPackFormat(writeMeta(t, tc.meta))
			if err != nil {
				t.Fatalf("ReadPackFormat: %v", err)
			}
			if got.Min != tc.wantMin || got.Max != tc.wantMax {
				t.Fatalf("got %v..%v, want %v..%v", got.Min, got.Max, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func TestPackFormatString(t *testing.T) {
	if got := (PackFormat{Min: 48, Max: 48}).String(); got != "48" {
		t.Fatalf("single = %q", got)
	}
	if got := (PackFormat{Min: 88, Max: 94.1}).String(); got != "88 - 94.1" {
		t.Fatalf("range = %q", got)
	}
	if got := (PackFormat{}).String(); got != "" {
		t.Fatalf("unknown = %q", got)
	}
}

func TestGamePackFormat(t *testing.T) {
	cases := []struct {
		version string
		format  float64
		known   bool
	}{
		{"1.21.1", 48, true},
		{"1.21", 48, true},
		{"1.20.4", 26, true},
		{"1.18.2", 9, true},
		{"26.3", 121, true},
		{"1.21.11", 94.1, true},
		{"24w14a", 0, false},  // snapshot: never guessed
		{"9.9.9", 0, false},   // future release
		{"", 0, false},
	}
	for _, tc := range cases {
		got, known := GamePackFormat(tc.version)
		if got != tc.format || known != tc.known {
			t.Errorf("GamePackFormat(%q) = %v,%v want %v,%v", tc.version, got, known, tc.format, tc.known)
		}
	}
}

func TestPackFormatWarning(t *testing.T) {
	// Newer pack on older server: warn and name the fix.
	msg := PackFormatWarning(PackFormat{Min: 57, Max: 57}, "1.21.1")
	if msg == "" {
		t.Fatal("want warning for a future pack format")
	}

	// Older or exact pack: silent.
	if msg := PackFormatWarning(PackFormat{Min: 26, Max: 26}, "1.21.1"); msg != "" {
		t.Fatalf("older pack must be silent, got %q", msg)
	}
	if msg := PackFormatWarning(PackFormat{Min: 48, Max: 48}, "1.21.1"); msg != "" {
		t.Fatalf("matching pack must be silent, got %q", msg)
	}

	// Unknown sides: silent (never warn on guesses).
	if msg := PackFormatWarning(PackFormat{Min: 57, Max: 57}, "24w14a"); msg != "" {
		t.Fatalf("unknown version must be silent, got %q", msg)
	}
	if msg := PackFormatWarning(PackFormat{}, "1.21.1"); msg != "" {
		t.Fatalf("unknown pack format must be silent, got %q", msg)
	}
}
