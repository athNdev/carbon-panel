// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package files

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func tarGz(t *testing.T, headers []tar.Header, bodies []string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for i, hdr := range headers {
		hdr := hdr
		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == 0 {
			hdr.Size = int64(len(bodies[i]))
		}
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Size > 0 {
			if _, err := tw.Write([]byte(bodies[i])); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestExtractTarGz(t *testing.T) {
	dest := t.TempDir()
	archive := tarGz(t,
		[]tar.Header{{Name: "world/level.dat", Mode: 0o644, Typeflag: tar.TypeReg}},
		[]string{"ok"},
	)
	if err := ExtractTarGz(archive, dest); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dest, "world", "level.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Fatalf("body %q", body)
	}

	escape := tarGz(t,
		[]tar.Header{{Name: "../outside.txt", Mode: 0o644, Typeflag: tar.TypeReg}},
		[]string{"no"},
	)
	if err := ExtractTarGz(escape, dest); err == nil {
		t.Fatal("parent path was extracted")
	}

	absolute := tarGz(t,
		[]tar.Header{{Name: filepath.Join(dest, "abs.txt"), Mode: 0o644, Typeflag: tar.TypeReg}},
		[]string{"no"},
	)
	if err := ExtractTarGz(absolute, dest); err == nil {
		t.Fatal("absolute path was extracted")
	}

	legacy := tarGz(t,
		[]tar.Header{{Name: "legacy.txt", Mode: 0o644, Typeflag: 0}},
		[]string{"old"},
	)
	if err := ExtractTarGz(legacy, dest); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(dest, "legacy.txt")); err != nil || string(body) != "old" {
		t.Fatalf("legacy typeflag: %v %q", err, body)
	}

	link := tarGz(t,
		[]tar.Header{{Name: "escape", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777}},
		[]string{""},
	)
	if err := ExtractTarGz(link, dest); err == nil {
		t.Fatal("symlink entry was extracted")
	}
	if _, err := os.Lstat(filepath.Join(dest, "escape")); !os.IsNotExist(err) {
		t.Fatalf("symlink materialised: %v", err)
	}
}
