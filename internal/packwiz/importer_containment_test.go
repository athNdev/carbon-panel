package packwiz

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

func zipWith(t *testing.T, entries map[string]string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

// TestImporterRejectsZipSlip probes all three importers (mrpack, CurseForge,
// packwiz zip). Each writes archive entries with filepath.Join(pDir, rel) and
// no boundary check, so a ".."-containing entry can escape the pack directory.
func TestImporterRejectsZipSlip(t *testing.T) {
	const mrIndex = `{"formatVersion":1,"game":"minecraft","name":"p","versionId":"1.0.0","dependencies":{"minecraft":"1.20.1"},"files":[]}`
	const cfManifest = `{"manifestType":"minecraftModpack","manifestVersion":1,"name":"p","version":"1.0.0","overrides":"overrides","minecraft":{"version":"1.20.1","modLoaders":[]},"files":[]}`

	cases := []struct {
		name    string
		entries map[string]string
		escape  string
		run     func(m *Manager, r *bytes.Reader) error
	}{
		{
			name: "mrpack overrides",
			entries: map[string]string{
				"modrinth.index.json":        mrIndex,
				"overrides/../../escaped-mr.txt": "pwned",
			},
			escape: "escaped-mr.txt",
			run: func(m *Manager, r *bytes.Reader) error {
				_, err := m.ImportMrpack(r, int64(r.Len()), ImportOptions{Name: "p"})
				return err
			},
		},
		{
			name: "curseforge overrides",
			entries: map[string]string{
				"manifest.json":                  cfManifest,
				"overrides/../../escaped-cf.txt":  "pwned",
			},
			escape: "escaped-cf.txt",
			run: func(m *Manager, r *bytes.Reader) error {
				_, err := m.ImportCurseForge(r, int64(r.Len()), ImportOptions{Name: "p"})
				return err
			},
		},
		{
			name: "packwiz zip root entries",
			entries: map[string]string{
				"pack.toml":            "name = \"p\"\n",
				"../../escaped-pw.txt": "pwned",
			},
			escape: "escaped-pw.txt",
			run: func(m *Manager, r *bytes.Reader) error {
				_, err := m.ImportPackwizZip(r, int64(r.Len()), ImportOptions{Name: "p"})
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			m := NewManager(filepath.Join(base, "packs"), logger.New())
			reader := zipWith(t, tc.entries)
			err := tc.run(m, reader)
			t.Logf("import err=%v", err)

			if _, statErr := os.Stat(filepath.Join(base, tc.escape)); statErr == nil {
				t.Fatalf("ZIP-SLIP CONFIRMED (%s): wrote outside the pack dir at %s", tc.name, filepath.Join(base, tc.escape))
			}
		})
	}
}
