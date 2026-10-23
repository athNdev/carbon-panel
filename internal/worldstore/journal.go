package worldstore

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type journal struct {
	ID       string   `json:"id"`
	Identity Identity `json:"identity"`
	Stage    string   `json:"stage"`
	Old      string   `json:"old"`
	HadOld   bool     `json:"had_old"`
	Phase    string   `json:"phase"`
	DataName string   `json:"data_name"`
}

type restoreFile struct {
	Path string
	Data []byte
	Mode os.FileMode
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (l *Local) journalPath(id string) string {
	return filepath.Join(l.journalDir, "worldstore-"+id+".json")
}

func (l *Local) saveJournal(j journal) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(l.journalDir, ".worldstore-journal-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(name)
		return err
	}
	if err = os.Rename(name, l.journalPath(j.ID)); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func (l *Local) removeJournal(id string) error {
	err := os.Remove(l.journalPath(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Recover replays local restore journals. An operation recorded as new_moved
// commits by deleting the old tree. Earlier phases roll back to the original
// tree, including crashes between a rename and the following journal update.
// Invalid or mismatched journals fail closed and are left for operator review.
func (l *Local) Recover() error {
	items, err := os.ReadDir(l.journalDir)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.IsDir() || !strings.HasPrefix(item.Name(), "worldstore-") || !strings.HasSuffix(item.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(l.journalDir, item.Name()))
		if err != nil {
			return err
		}
		var j journal
		if err := json.Unmarshal(b, &j); err != nil {
			return fmt.Errorf("read worldstore journal %s: %w", item.Name(), err)
		}
		if !validID(j.ID) || item.Name() != "worldstore-"+j.ID+".json" || j.Identity != l.identity || j.DataName != l.dataName || j.Stage != ".worldstore-stage-"+j.ID || j.Old != ".worldstore-old-"+j.ID {
			return fmt.Errorf("worldstore: invalid or foreign journal %s", item.Name())
		}
		if err := l.recoverOne(j); err != nil {
			return fmt.Errorf("recover %s: %w", j.ID, err)
		}
	}
	return nil
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (l *Local) recoverOne(j journal) error {
	if j.Phase == "new_moved" || j.Phase == "complete" {
		if j.HadOld {
			if err := l.root.RemoveAll(j.Old); err != nil {
				return err
			}
		}
		if err := l.root.RemoveAll(j.Stage); err != nil {
			return err
		}
		return l.removeJournal(j.ID)
	}
	return l.rollback(j)
}

func (l *Local) rollback(j journal) error {
	stageExists := existsRoot(l.root, j.Stage)
	oldExists := existsRoot(l.root, j.Old)
	worldExists := existsRoot(l.root, j.DataName)
	if j.HadOld && oldExists {
		// If the staged tree was renamed into place before the phase update,
		// remove it first and restore the previous exact tree.
		if worldExists {
			if err := l.root.RemoveAll(j.DataName); err != nil {
				return err
			}
		}
		if err := l.root.Rename(j.Old, j.DataName); err != nil {
			return err
		}
	} else if !j.HadOld && !stageExists && worldExists && (j.Phase == "old_moved" || j.Phase == "extracted" || j.Phase == "intent") {
		// No previous world existed; a missing stage plus a present world means
		// the stage->world rename happened before its journal update.
		if err := l.root.RemoveAll(j.DataName); err != nil {
			return err
		}
	}
	if err := l.root.RemoveAll(j.Stage); err != nil {
		return err
	}
	if err := l.root.RemoveAll(j.Old); err != nil {
		return err
	}
	return l.removeJournal(j.ID)
}

func existsRoot(root *os.Root, name string) bool { _, err := root.Lstat(name); return err == nil }

func decodeArchive(data []byte) ([]restoreFile, error) {
	if len(data) == 0 || len(data) > maxArchiveBytes {
		return nil, errors.New("worldstore: archive size outside limits")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if len(zr.File) == 0 || len(zr.File) > maxEntries+1 {
		return nil, errors.New("worldstore: archive entry count outside limits")
	}
	files := make(map[string]restoreFile)
	var manifestBytes []byte
	var total int64
	for _, zf := range zr.File {
		name := strings.TrimSuffix(zf.Name, "/")
		if name == ".worldstore-manifest.json" {
			if manifestBytes != nil {
				return nil, errors.New("worldstore: duplicate manifest")
			}
			if !zf.Mode().IsRegular() || zf.Mode()&os.ModeSymlink != 0 {
				return nil, errors.New("worldstore: invalid manifest entry")
			}
			if zf.UncompressedSize64 > 1<<20 {
				return nil, errors.New("worldstore: manifest too large")
			}
			f, e := zf.Open()
			if e != nil {
				return nil, e
			}
			manifestBytes, e = io.ReadAll(io.LimitReader(f, 1<<20+1))
			ce := f.Close()
			if e != nil {
				return nil, e
			}
			if ce != nil {
				return nil, ce
			}
			if len(manifestBytes) > 1<<20 {
				return nil, errors.New("worldstore: manifest too large")
			}
			continue
		}
		p, e := cleanRelative(name)
		if e != nil || p != name {
			return nil, fmt.Errorf("%w: archive entry %q", ErrPath, zf.Name)
		}
		if strings.HasSuffix(zf.Name, "/") {
			if !zf.Mode().IsDir() {
				return nil, fmt.Errorf("worldstore: invalid directory entry %q", zf.Name)
			}
			continue
		}
		if _, ok := files[p]; ok {
			return nil, fmt.Errorf("worldstore: duplicate archive entry %q", p)
		}
		if zf.Mode()&os.ModeSymlink != 0 || !zf.Mode().IsRegular() {
			return nil, fmt.Errorf("worldstore: non-regular archive entry %q", p)
		}
		if zf.UncompressedSize64 > uint64(maxFileBytes) {
			return nil, fmt.Errorf("worldstore: file too large: %s", p)
		}
		f, e := zf.Open()
		if e != nil {
			return nil, e
		}
		b, e := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
		ce := f.Close()
		if e != nil {
			return nil, e
		}
		if ce != nil {
			return nil, ce
		}
		if int64(len(b)) > maxFileBytes {
			return nil, fmt.Errorf("worldstore: expanded file too large: %s", p)
		}
		total += int64(len(b))
		if total > maxWorldBytes {
			return nil, errors.New("worldstore: expanded archive exceeds limit")
		}
		files[p] = restoreFile{Path: p, Data: b, Mode: 0600}
	}
	if manifestBytes == nil {
		return nil, errors.New("worldstore: backup manifest missing")
	}
	var manifest struct {
		Version int     `json:"version"`
		Files   []Entry `json:"files"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, err
	}
	if manifest.Version != 1 {
		return nil, errors.New("worldstore: unsupported manifest version")
	}
	if len(manifest.Files) != len(files) {
		return nil, errors.New("worldstore: manifest file set mismatch")
	}
	for _, e := range manifest.Files {
		p, err := cleanRelative(e.Path)
		if err != nil || p != e.Path {
			return nil, ErrPath
		}
		f, ok := files[e.Path]
		if !ok {
			return nil, fmt.Errorf("worldstore: manifest file missing: %s", e.Path)
		}
		sum := sha256.Sum256(f.Data)
		if e.Size != int64(len(f.Data)) || e.SHA256 != hex.EncodeToString(sum[:]) {
			return nil, fmt.Errorf("worldstore: checksum mismatch: %s", e.Path)
		}
	}
	out := make([]restoreFile, 0, len(files))
	for _, f := range files {
		out = append(out, f)
	}
	return out, nil
}

func extractFiles(root *os.Root, stage string, files []restoreFile) error {
	for _, item := range files {
		p := filepath.ToSlash(filepath.Join(stage, filepath.FromSlash(item.Path)))
		parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(p)))
		if parent != stage {
			if err := root.MkdirAll(parent, 0700); err != nil {
				return err
			}
		}
		f, err := root.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, item.Mode)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(item.Data)
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
