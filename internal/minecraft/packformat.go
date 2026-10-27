// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package minecraft

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// PackFormat is the pack-format range a datapack declares in pack.mcmeta.
// Formats since 1.21.9 carry minor parts (88.0, 94.1), so the range is
// floating point. A zero Min means the archive declared nothing usable.
type PackFormat struct {
	Min float64
	Max float64
}

// Known reports whether the archive declared any format at all.
func (p PackFormat) Known() bool { return p.Min > 0 }

// String renders the declared range for display ("48", "88.0 - 94.1").
func (p PackFormat) String() string {
	if !p.Known() {
		return ""
	}
	if p.Min == p.Max {
		return formatNumber(p.Min)
	}
	return fmt.Sprintf("%s - %s", formatNumber(p.Min), formatNumber(p.Max))
}

func formatNumber(v float64) string {
	s := fmt.Sprintf("%.1f", v)
	return strings.TrimSuffix(s, ".0")
}

// gamePackFormats maps Minecraft release lines to the data pack format each
// release supports. Source: minecraft.wiki/w/Pack_format (data pack format
// history). Snapshots are deliberately absent: unknown versions report
// unknown instead of guessing.
var gamePackFormats = []struct {
	min, max string // inclusive release range
	format   float64
}{
	{"1.13", "1.14.4", 4},
	{"1.15", "1.16.1", 5},
	{"1.16.2", "1.16.5", 6},
	{"1.17", "1.17.1", 7},
	{"1.18", "1.18.1", 8},
	{"1.18.2", "1.18.2", 9},
	{"1.19", "1.19.3", 10},
	{"1.19.4", "1.19.4", 12},
	{"1.20", "1.20.1", 15},
	{"1.20.2", "1.20.2", 18},
	{"1.20.3", "1.20.4", 26},
	{"1.20.5", "1.20.6", 41},
	{"1.21", "1.21.1", 48},
	{"1.21.2", "1.21.3", 57},
	{"1.21.4", "1.21.4", 61},
	{"1.21.5", "1.21.5", 71},
	{"1.21.6", "1.21.6", 80},
	{"1.21.7", "1.21.8", 81},
	{"1.21.9", "1.21.10", 88},
	{"1.21.11", "1.21.11", 94.1},
	{"26.1", "26.1.2", 101.1},
	{"26.2", "26.2", 107.1},
	{"26.3", "26.3", 121},
}

// GamePackFormat reports the data pack format supported by a Minecraft
// release (e.g. "1.21.1" -> 48). Unknown versions (snapshots, future
// releases) report false.
func GamePackFormat(mcVersion string) (float64, bool) {
	v := normalizeVersion(mcVersion)
	if v == "" {
		return 0, false
	}
	for _, row := range gamePackFormats {
		if compareVersions(row.min, v) <= 0 && compareVersions(v, row.max) <= 0 {
			return row.format, true
		}
	}
	return 0, false
}

// ReadPackFormat parses the pack.mcmeta of a datapack archive. It understands
// all three historical shapes: legacy pack_format, pack_format as a range
// object with supported_formats, and the 1.21.9+ min_format/max_format pair.
// A zero value means the archive declared nothing the panel can judge.
func ReadPackFormat(archivePath string) (PackFormat, error) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return PackFormat{}, fmt.Errorf("not a valid zip archive: %w", err)
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		name := strings.TrimPrefix(filepath.ToSlash(f.Name), "./")
		if name != "pack.mcmeta" || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return PackFormat{}, fmt.Errorf("read pack.mcmeta: %w", err)
		}
		defer func() { _ = rc.Close() }()
		var meta struct {
			Pack struct {
				PackFormat       any `json:"pack_format"`
				SupportedFormats any `json:"supported_formats"`
				MinFormat        any `json:"min_format"`
				MaxFormat        any `json:"max_format"`
			} `json:"pack"`
		}
		if err := json.NewDecoder(rc).Decode(&meta); err != nil {
			return PackFormat{}, fmt.Errorf("parse pack.mcmeta: %w", err)
		}
		if min, max, ok := rangeValue(meta.Pack.MinFormat, meta.Pack.MaxFormat); ok {
			return PackFormat{Min: min, Max: max}, nil
		}
		if min, max, ok := rangeValue(meta.Pack.SupportedFormats, meta.Pack.SupportedFormats); ok {
			return PackFormat{Min: min, Max: max}, nil
		}
		if min, max, ok := rangeValue(meta.Pack.PackFormat, meta.Pack.PackFormat); ok {
			return PackFormat{Min: min, Max: max}, nil
		}
		return PackFormat{}, nil
	}
	return PackFormat{}, fmt.Errorf("archive has no pack.mcmeta")
}

// rangeValue reads a format value that is either a plain number or an object
// with min_inclusive/max_inclusive. For the min_format/max_format pair the
// two arguments differ; for single fields they are the same value.
func rangeValue(minRaw, maxRaw any) (float64, float64, bool) {
	min, ok1 := scalarFormat(minRaw)
	max, ok2 := scalarFormat(maxRaw)
	if ok1 || ok2 {
		if !ok1 {
			min = max
		}
		if !ok2 {
			max = min
		}
		if max < min {
			min, max = max, min
		}
		return min, max, true
	}
	if obj, ok := minRaw.(map[string]any); ok {
		lo, okLo := scalarFormat(obj["min_inclusive"])
		hi, okHi := scalarFormat(obj["max_inclusive"])
		if okLo || okHi {
			if !okLo {
				lo = hi
			}
			if !okHi {
				hi = lo
			}
			return lo, hi, true
		}
	}
	return 0, 0, false
}

func scalarFormat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		if n > 0 {
			return n, true
		}
	case json.Number:
		f, err := n.Float64()
		if err == nil && f > 0 {
			return f, true
		}
	}
	return 0, false
}

// PackFormatWarning judges a datapack's declared formats against a server's
// Minecraft version. Minecraft loads packs whose format is at or below the
// game's supported format (older packs get fixers) and marks packs built for
// NEWER formats as incompatible. The message is empty when the pack is fine
// or when either side is unknown — the panel never warns on guesses.
func PackFormatWarning(pack PackFormat, mcVersion string) string {
	gameFormat, known := GamePackFormat(mcVersion)
	if !known || !pack.Known() {
		return ""
	}
	if pack.Min > gameFormat {
		return fmt.Sprintf(
			"pack format %s targets Minecraft %s or newer; this server on %s supports %s and will mark it incompatible",
			pack.String(), firstVersionForFormat(pack.Min), mcVersion, formatNumber(gameFormat),
		)
	}
	return ""
}

// firstVersionForFormat is a display helper naming the first release that
// supports a format.
func firstVersionForFormat(format float64) string {
	for _, row := range gamePackFormats {
		if row.format == format {
			return row.min
		}
		if row.format > format {
			return row.min
		}
	}
	return "a newer Minecraft"
}

// normalizeVersion trims a release string to its numeric dotted parts
// ("v1.21.1 " -> "1.21.1"); non-release strings (snapshots) return "".
func normalizeVersion(v string) string {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if v == "" {
		return ""
	}
	parts := strings.Split(v, ".")
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return ""
		}
	}
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	return strings.Join(parts, ".")
}

// compareVersions compares dotted numeric versions (-1, 0, 1).
func compareVersions(a, b string) int {
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	for i := 0; i < len(ap) || i < len(bp); i++ {
		av, bv := 0, 0
		if i < len(ap) {
			av, _ = strconv.Atoi(ap[i])
		}
		if i < len(bp) {
			bv, _ = strconv.Atoi(bp[i])
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}
