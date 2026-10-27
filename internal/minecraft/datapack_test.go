// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package minecraft

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetDatapacksPath(t *testing.T) {
	tests := []struct {
		name      string
		levelName string
		wantTail  string
	}{
		{"default world", "", filepath.Join("world", "datapacks")},
		{"custom level", "myworld", filepath.Join("myworld", "datapacks")},
		{"traversal blocked", "../etc", filepath.Join("world", "datapacks")},
		{"absolute blocked", "/etc", filepath.Join("world", "datapacks")},
		{"dot blocked", ".", filepath.Join("world", "datapacks")},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			got := GetDatapacksPath("/data/srv", c.levelName)
			if !strings.HasSuffix(got, c.wantTail) {
				t.Errorf("GetDatapacksPath(_, %q) = %q, want suffix %q", c.levelName, got, c.wantTail)
			}
			// Never escape the server root.
			if !strings.HasPrefix(got, filepath.Clean("/data/srv")) {
				t.Errorf("path %q escapes the server root", got)
			}
		})
	}
}

func TestIsValidDatapackFile(t *testing.T) {
	valid := []string{"pack.zip", "PACK.ZIP", "my-datapack.zip"}
	for _, n := range valid {
		if !IsValidDatapackFile(n) {
			t.Errorf("IsValidDatapackFile(%q) = false, want true", n)
		}
	}
	invalid := []string{"pack.jar", "pack.tar", "pack", "pack.zip.txt", ""}
	for _, n := range invalid {
		if IsValidDatapackFile(n) {
			t.Errorf("IsValidDatapackFile(%q) = true, want false", n)
		}
	}
}

func writeZipWithEntries(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDatapackArchive(t *testing.T) {
	dir := t.TempDir()

	// Valid: has pack.mcmeta.
	good := filepath.Join(dir, "good.zip")
	writeZipWithEntries(t, good, map[string]string{
		"pack.mcmeta":      `{"pack":{"pack_format":34}}`,
		"data/minecraft/x": "",
	})
	if err := ValidateDatapackArchive(good); err != nil {
		t.Errorf("valid datapack rejected: %v", err)
	}

	// Invalid: no pack.mcmeta.
	noMeta := filepath.Join(dir, "nometa.zip")
	writeZipWithEntries(t, noMeta, map[string]string{"data/foo": ""})
	if err := ValidateDatapackArchive(noMeta); err == nil {
		t.Error("zip without pack.mcmeta accepted; want rejection")
	}

	// Invalid: not a zip.
	notZip := filepath.Join(dir, "not.zip")
	if err := os.WriteFile(notZip, []byte("plain text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDatapackArchive(notZip); err == nil {
		t.Error("non-zip accepted; want rejection")
	}
}

func TestNormalizeDatapackArchive(t *testing.T) {
	dir := t.TempDir()

	t.Run("wrapper folder is stripped", func(t *testing.T) {
		p := filepath.Join(dir, "wrapped.zip")
		writeZipWithEntries(t, p, map[string]string{
			"my-pack/pack.mcmeta":      `{"pack":{"pack_format":34}}`,
			"my-pack/data/minecraft/x": "hello",
			"my-pack/.DS_Store":        "junk",
		})
		normalized, err := NormalizeDatapackArchive(p)
		if err != nil || !normalized {
			t.Fatalf("normalized=%v err=%v", normalized, err)
		}
		if err := ValidateDatapackArchive(p); err != nil {
			t.Fatalf("still invalid after normalize: %v", err)
		}
		r, err := zip.OpenReader(p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Close() }()
		var names []string
		for _, f := range r.File {
			names = append(names, f.Name)
		}
		want := map[string]bool{"pack.mcmeta": true, "data/minecraft/x": true, ".DS_Store": true}
		if len(names) != len(want) {
			t.Fatalf("entries=%v", names)
		}
		for _, n := range names {
			if !want[n] {
				t.Errorf("unexpected entry %q", n)
			}
		}
	})

	t.Run("valid archive untouched", func(t *testing.T) {
		p := filepath.Join(dir, "good.zip")
		writeZipWithEntries(t, p, map[string]string{"pack.mcmeta": `{}`})
		normalized, err := NormalizeDatapackArchive(p)
		if err != nil || normalized {
			t.Fatalf("normalized=%v err=%v; want false,nil", normalized, err)
		}
	})

	t.Run("no pack.mcmeta anywhere", func(t *testing.T) {
		p := filepath.Join(dir, "empty.zip")
		writeZipWithEntries(t, p, map[string]string{"a/b.txt": "x"})
		if _, err := NormalizeDatapackArchive(p); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("multiple wrapper folders", func(t *testing.T) {
		p := filepath.Join(dir, "multi.zip")
		writeZipWithEntries(t, p, map[string]string{
			"a/pack.mcmeta": `{}`,
			"b/pack.mcmeta": `{}`,
		})
		if _, err := NormalizeDatapackArchive(p); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("traversal entries rejected", func(t *testing.T) {
		p := filepath.Join(dir, "evil.zip")
		writeZipWithEntries(t, p, map[string]string{
			"w/pack.mcmeta":      `{}`,
			"w/../../etc/passwd": "x",
		})
		if _, err := NormalizeDatapackArchive(p); err == nil {
			t.Fatal("want error")
		}
	})
}

func TestValidateDatapackFilename(t *testing.T) {
	valid := []string{"pack.zip", "my-datapack.zip", "PACK.ZIP"}
	for _, n := range valid {
		if err := ValidateDatapackFilename(n); err != nil {
			t.Errorf("ValidateDatapackFilename(%q) = %v, want nil", n, err)
		}
	}

	// Path traversal and non-zip must be rejected.
	invalid := []string{
		"", ".", "..", "/etc/passwd.zip", "../evil.zip", `..\evil.zip`,
		"dir/pack.zip", "pack.jar", "pack.tar", "pack",
	}
	for _, n := range invalid {
		if err := ValidateDatapackFilename(n); err == nil {
			t.Errorf("ValidateDatapackFilename(%q) = nil, want rejection", n)
		}
	}
}

func zipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDownloadAllowsPrivate(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"http://10.0.0.1:8080/api", true},     // LAN self-hosted mirror
		{"http://192.168.1.5/api", true},       // RFC1918
		{"http://127.0.0.1:9000/api", true},    // loopback mirror (test deployments)
		{"http://localhost:9000/api", true},    // resolves to loopback
		{"https://api.modrinth.com/v2", false}, // public index: strict downloads
		{"http://8.8.8.8/api", false},          // public IP literal
		{"not a url", false},                   // fail closed
		{"", false},                            // no origin reported: fail closed
		{"http://invalid.invalid/api", false},  // unresolvable: fail closed
	}
	for _, tc := range cases {
		if got := DownloadAllowsPrivate(tc.url); got != tc.want {
			t.Errorf("DownloadAllowsPrivate(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestDownloadDatapackArchive(t *testing.T) {
	good := zipBytes(t, map[string]string{"pack.mcmeta": "{}"})

	t.Run("downloads a datapack", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(good)
		}))
		defer srv.Close()

		dest := filepath.Join(t.TempDir(), "pack.zip")
		if err := DownloadDatapackArchiveWithClient(srv.Client(), context.Background(), srv.URL, dest); err != nil {
			t.Fatalf("DownloadDatapackArchive: %v", err)
		}
		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, good) {
			t.Error("downloaded bytes differ from source")
		}
	})

	t.Run("rejects non-2xx", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		dest := filepath.Join(t.TempDir(), "pack.zip")
		if err := DownloadDatapackArchiveWithClient(srv.Client(), context.Background(), srv.URL, dest); err == nil {
			t.Fatal("expected an error for a 404 response")
		}
	})

	t.Run("enforces the size ceiling", func(t *testing.T) {
		// Stream well past the cap in small chunks (no huge allocation).
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			chunk := make([]byte, 64*1024)
			for i := 0; i < MaxDatapackBytes/len(chunk)+2; i++ {
				if _, err := w.Write(chunk); err != nil {
					return
				}
			}
		}))
		defer srv.Close()

		dest := filepath.Join(t.TempDir(), "big.zip")
		err := DownloadDatapackArchiveWithClient(srv.Client(), context.Background(), srv.URL, dest)
		if err == nil {
			t.Fatal("expected an error when the datapack exceeds the size cap")
		}
		if !strings.Contains(err.Error(), "exceeds") {
			t.Errorf("unexpected error: %v", err)
		}
		if _, statErr := os.Stat(dest); statErr == nil {
			t.Error("oversized download left a file behind")
		}
	})
}
