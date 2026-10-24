package worldstore

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) (*Local, Identity, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "server")
	journal := filepath.Join(base, "journal")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	id := Identity{Server: "srv-1", Node: "node-1", Daemon: "daemon-1", Owner: "owner-1", Generation: 7}
	s, err := NewLocal(id, root, journal)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, s.Identity(), root
}

func TestBackupRestorePreservesNamedWorldContentsAndHashes(t *testing.T) {
	s, id, root := testStore(t)
	if err := os.MkdirAll(filepath.Join(root, "region"), 0700); err != nil {
		t.Fatal(err)
	}
	want := map[string][]byte{"level.dat": []byte("level bytes"), "region/r.0.0.mca": []byte("region bytes"), "playerdata/p.dat": []byte("player bytes")}
	for p, b := range want {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := s.Backup(id)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(a.Data)
	if a.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("archive checksum = %s", a.SHA256)
	}
	if err := os.WriteFile(filepath.Join(root, "level.dat"), []byte("mutated"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(id, a.Data); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(want) {
		t.Fatalf("entries = %#v", entries)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(e.Path)))
		if err != nil {
			t.Fatal(err)
		}
		expected, ok := want[e.Path]
		if !ok || !bytes.Equal(data, expected) {
			t.Fatalf("%s content = %q, want %q", e.Path, data, expected)
		}
		h := sha256.Sum256(expected)
		if e.SHA256 != hex.EncodeToString(h[:]) {
			t.Fatalf("%s hash = %s", e.Path, e.SHA256)
		}
	}
}

func TestRestoreRemovesObsoleteWorldFiles(t *testing.T) {
	s, id, root := testStore(t)
	_ = os.WriteFile(filepath.Join(root, "obsolete.dat"), []byte("old"), 0600)
	a := testArchive(t, map[string][]byte{"keep.dat": []byte("new")})
	if err := s.Restore(id, a); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "obsolete.dat")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("obsolete file remains: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "keep.dat"))
	if err != nil || string(got) != "new" {
		t.Fatalf("keep.dat=%q err=%v", got, err)
	}
}

func TestFailedExtractionPreservesOldWorld(t *testing.T) {
	s, id, root := testStore(t)
	old := []byte("old world")
	_ = os.WriteFile(filepath.Join(root, "level.dat"), old, 0600)
	// A file and a descendant cannot coexist on disk. The archive passes
	// checksum/manifest validation, then extraction fails while the old world
	// remains in place because the swap has not started.
	archive := testArchive(t, map[string][]byte{"collision": []byte("file"), "collision/child": []byte("child")})
	if err := s.Restore(id, archive); err == nil {
		t.Fatal("expected extraction failure")
	}
	got, err := os.ReadFile(filepath.Join(root, "level.dat"))
	if err != nil || !bytes.Equal(got, old) {
		t.Fatalf("old world changed: %q, %v", got, err)
	}
}

func TestWrongIdentityAndStaleGenerationRejected(t *testing.T) {
	s, id, _ := testStore(t)
	wrong := id
	wrong.Node = "other-node"
	if _, err := s.List(wrong); !errors.Is(err, ErrIdentity) {
		t.Fatalf("wrong node error=%v", err)
	}
	stale := id
	stale.Generation--
	if _, err := s.List(stale); !errors.Is(err, ErrIdentity) {
		t.Fatalf("stale generation error=%v", err)
	}
}

func TestRecoverRollsBackInterruptionAfterStageRename(t *testing.T) {
	s, id, root := testStore(t)
	oldName := ".worldstore-old-0123456789abcdef0123456789abcdef"
	parent := filepath.Dir(root)
	if err := os.MkdirAll(filepath.Join(parent, oldName), 0700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(parent, oldName, "level.dat"), []byte("old"), 0600)
	_ = os.WriteFile(filepath.Join(root, "level.dat"), []byte("new"), 0600)
	j := journal{ID: "0123456789abcdef0123456789abcdef", Identity: id, Stage: ".worldstore-stage-0123456789abcdef0123456789abcdef", Old: oldName, HadOld: true, Phase: "old_moved", DataName: filepath.Base(root)}
	if err := s.saveJournal(j); err != nil {
		t.Fatal(err)
	}
	if err := s.Recover(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "level.dat"))
	if err != nil || string(got) != "old" {
		t.Fatalf("recovered world=%q err=%v", got, err)
	}
	if _, err := os.Stat(s.journalPath(j.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal remains: %v", err)
	}
}

func testArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	entries := make([]Entry, 0, len(files))
	for p, b := range files {
		h := sha256.Sum256(b)
		entries = append(entries, Entry{Path: p, Size: int64(len(b)), SHA256: hex.EncodeToString(h[:])})
	}
	manifest, _ := json.Marshal(struct {
		Version int     `json:"version"`
		Files   []Entry `json:"files"`
	}{1, entries})
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for p, data := range files {
		w, err := z.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	w, err := z.Create(".worldstore-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(manifest); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestRejectsSymlinkEscapeAndTraversal(t *testing.T) {
	s, id, root := testStore(t)
	if err := os.MkdirAll(filepath.Join(root), 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "link")); err == nil {
		if _, err := s.Read(id, "link/secret"); err == nil {
			t.Fatal("read through symlink should fail")
		}
	}
	if _, err := s.Read(id, "../secret"); !errors.Is(err, ErrPath) {
		t.Fatalf("traversal error=%v", err)
	}
}

var _ = io.EOF
