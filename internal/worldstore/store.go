// Package worldstore provides a node-scoped local filesystem implementation
// for server world operations. The package is transport-neutral; callers pass
// the complete node and ownership identity on every operation so a worker can
// be attached to a container or another remote transport later.
package worldstore

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxArchiveBytes = 512 << 20
	maxFileBytes    = 128 << 20
	maxWorldBytes   = 2 << 30
	maxEntries      = 100_000
)

var (
	ErrIdentity = errors.New("worldstore: identity mismatch")
	ErrPath     = errors.New("worldstore: invalid or unsafe path")
)

// Identity binds an operation to one server, node, daemon, owner, generation,
// and authorized root. AuthorizedRoot is canonicalized to an absolute path by
// NewLocal. Generation is part of equality, so workers from an older server
// assignment fail closed after reassignment.
type Identity struct {
	Server         string `json:"server"`
	Node           string `json:"node"`
	Daemon         string `json:"daemon"`
	Owner          string `json:"owner"`
	Generation     uint64 `json:"generation"`
	AuthorizedRoot string `json:"authorized_root"`
}

// Entry describes one regular file in an authorized server data tree. Directories are implicit.
type Entry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Archive is a deterministic ZIP backup and its whole-archive SHA-256.
type Archive struct {
	Data   []byte `json:"data"`
	SHA256 string `json:"sha256"`
}

// Store exposes filesystem operations against the single authorized root
// captured at construction. Each method requires the caller's complete
// identity; no operation accepts a host path. ZIP restore accepts only regular
// files with safe relative names and a verified manifest. Limits are fixed to
// bound memory and decompression work. Restore replaces the complete data root
// through sibling renames and a recoverable journal. Journals are
// atomically replaced, but this implementation does not claim power-loss
// durability because it does not fsync files/directories.
type Store interface {
	List(identity Identity) ([]Entry, error)
	Read(identity Identity, path string) ([]byte, error)
	Upload(identity Identity, path string, data []byte) error
	Backup(identity Identity) (Archive, error)
	Restore(identity Identity, archive []byte) error
	Recover() error
}

type Local struct {
	root       *os.Root // parent of dataName; user paths are always scoped below dataName
	dataName   string
	identity   Identity
	journalDir string
}

// NewLocal opens the parent of authorizedRoot as an os.Root and pins the
// authorized root's basename. User paths are opened only below that basename;
// this also lets restore stage and rename sibling trees without allowing a
// request to select an arbitrary host path. The journal directory must be
// outside authorizedRoot and private to this worker instance.
func NewLocal(identity Identity, authorizedRoot, journalDir string) (*Local, error) {
	if identity.Server == "" || identity.Node == "" || identity.Daemon == "" || identity.Owner == "" || identity.Generation == 0 {
		return nil, errors.New("worldstore: identity fields and nonzero generation are required")
	}
	absRoot, err := filepath.Abs(authorizedRoot)
	if err != nil {
		return nil, err
	}
	absRoot, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve authorized root: %w", err)
	}
	identity.AuthorizedRoot = filepath.Clean(absRoot)
	if err := os.MkdirAll(journalDir, 0700); err != nil {
		return nil, err
	}
	jd, err := filepath.Abs(journalDir)
	if err != nil {
		return nil, err
	}
	jd, err = filepath.EvalSymlinks(jd)
	if err != nil {
		return nil, err
	}
	relJournal, err := filepath.Rel(absRoot, jd)
	if err != nil {
		return nil, err
	}
	if relJournal == "." || (relJournal != ".." && !strings.HasPrefix(relJournal, ".."+string(filepath.Separator))) {
		return nil, errors.New("worldstore: journal directory must be outside authorized root")
	}
	parent, dataName := filepath.Dir(absRoot), filepath.Base(absRoot)
	r, err := os.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	info, err := r.Stat(dataName)
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	if !info.IsDir() {
		_ = r.Close()
		return nil, errors.New("worldstore: authorized root must be a directory")
	}
	l := &Local{root: r, dataName: dataName, identity: identity, journalDir: jd}
	if err := l.Recover(); err != nil {
		_ = r.Close()
		return nil, err
	}
	return l, nil
}

// Identity returns the canonical identity bound to this worker.
func (l *Local) Identity() Identity { return l.identity }

// Close releases the root handle owned by this local worker.
func (l *Local) Close() error { return l.root.Close() }

func (l *Local) check(id Identity) error {
	if id != l.identity {
		return ErrIdentity
	}
	return nil
}

func (l *Local) List(id Identity) ([]Entry, error) {
	if err := l.check(id); err != nil {
		return nil, err
	}
	w, err := l.root.OpenRoot(l.dataName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = w.Close() }()
	var entries []Entry
	err = fs.WalkDir(w.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink in authorized tree: %s", ErrPath, name)
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: non-regular file: %s", ErrPath, name)
		}
		f, err := w.Open(name)
		if err != nil {
			return err
		}
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(f, maxFileBytes+1))
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if n > maxFileBytes {
			return fmt.Errorf("worldstore: file exceeds limit: %s", name)
		}
		entries = append(entries, Entry{Path: name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))})
		if len(entries) > maxEntries {
			return errors.New("worldstore: too many files")
		}
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, err
}

func (l *Local) Read(id Identity, path string) ([]byte, error) {
	if err := l.check(id); err != nil {
		return nil, err
	}
	p, err := cleanRelative(path)
	if err != nil {
		return nil, err
	}
	w, err := l.root.OpenRoot(l.dataName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = w.Close() }()
	if err := rejectSymlinkComponents(w, p); err != nil {
		return nil, err
	}
	f, err := w.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err == nil && int64(len(b)) > maxFileBytes {
		return nil, errors.New("worldstore: file exceeds read limit")
	}
	return b, err
}

func (l *Local) Upload(id Identity, path string, data []byte) error {
	if err := l.check(id); err != nil {
		return err
	}
	if int64(len(data)) > maxFileBytes {
		return errors.New("worldstore: upload exceeds file limit")
	}
	p, err := cleanRelative(path)
	if err != nil {
		return err
	}
	w, err := l.root.OpenRoot(l.dataName)
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(p)))
	if parent != "." {
		if err := w.MkdirAll(parent, 0700); err != nil {
			return err
		}
	}
	if err := rejectSymlinkComponents(w, p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	tmp := p + ".worldstore-upload-tmp"
	if err := w.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := w.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		_ = w.Remove(tmp)
		return writeErr
	}
	if closeErr != nil {
		_ = w.Remove(tmp)
		return closeErr
	}
	if err := w.Rename(tmp, p); err != nil {
		_ = w.Remove(tmp)
		return err
	}
	return nil
}

func (l *Local) Backup(id Identity) (Archive, error) {
	entries, err := l.List(id)
	if err != nil {
		return Archive{}, err
	}
	var total int64
	manifest := struct {
		Version int     `json:"version"`
		Files   []Entry `json:"files"`
	}{Version: 1, Files: make([]Entry, 0, len(entries))}
	var b boundedBuffer
	z := zip.NewWriter(&b)
	for _, e := range entries {
		total += e.Size
		if total > maxWorldBytes {
			return Archive{}, errors.New("worldstore: world exceeds backup limit")
		}
		data, err := l.Read(id, e.Path)
		if err != nil {
			return Archive{}, err
		}
		total += int64(len(data)) - e.Size
		if total > maxWorldBytes {
			return Archive{}, errors.New("worldstore: world exceeds backup limit")
		}
		fileHash := sha256.Sum256(data)
		manifest.Files = append(manifest.Files, Entry{Path: e.Path, Size: int64(len(data)), SHA256: hex.EncodeToString(fileHash[:])})
		h := &zip.FileHeader{Name: e.Path, Method: zip.Deflate}
		h.SetMode(0600)
		h.Modified = time.Unix(0, 0).UTC()
		w, err := z.CreateHeader(h)
		if err != nil {
			return Archive{}, err
		}
		if _, err := w.Write(data); err != nil {
			return Archive{}, err
		}
	}
	mb, _ := json.Marshal(manifest)
	h := &zip.FileHeader{Name: ".worldstore-manifest.json", Method: zip.Deflate}
	h.SetMode(0600)
	h.Modified = time.Unix(0, 0).UTC()
	mw, err := z.CreateHeader(h)
	if err != nil {
		return Archive{}, err
	}
	if _, err := mw.Write(mb); err != nil {
		return Archive{}, err
	}
	if err := z.Close(); err != nil {
		return Archive{}, err
	}
	sum := sha256.Sum256(b.Bytes())
	return Archive{Data: b.Bytes(), SHA256: hex.EncodeToString(sum[:])}, nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if int64(b.Len())+int64(len(p)) > maxArchiveBytes {
		return 0, errors.New("worldstore: backup archive exceeds limit")
	}
	return b.Buffer.Write(p)
}

func (l *Local) Restore(id Identity, data []byte) error {
	if err := l.check(id); err != nil {
		return err
	}
	files, err := decodeArchive(data)
	if err != nil {
		return err
	}
	operationID, err := newID()
	if err != nil {
		return err
	}
	stage, old := ".worldstore-stage-"+operationID, ".worldstore-old-"+operationID
	j := journal{ID: operationID, Identity: l.identity, Stage: stage, Old: old, Phase: "intent", DataName: l.dataName}
	if _, err := l.root.Stat(l.dataName); err == nil {
		j.HadOld = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := l.saveJournal(j); err != nil {
		return err
	}
	if err := l.root.Mkdir(stage, 0700); err != nil {
		return err
	}
	if err := extractFiles(l.root, stage, files); err != nil {
		_ = l.root.RemoveAll(stage)
		_ = l.removeJournal(j.ID)
		return err
	}
	j.Phase = "extracted"
	if err := l.saveJournal(j); err != nil {
		_ = l.root.RemoveAll(stage)
		return err
	}
	if j.HadOld {
		if err := l.root.Rename(l.dataName, old); err != nil {
			_ = l.root.RemoveAll(stage)
			_ = l.removeJournal(j.ID)
			return err
		}
	}
	j.Phase = "old_moved"
	if err := l.saveJournal(j); err != nil {
		return err
	}
	if err := l.root.Rename(stage, l.dataName); err != nil {
		_ = l.rollback(j)
		return err
	}
	j.Phase = "new_moved"
	if err := l.saveJournal(j); err != nil {
		return err
	}
	if j.HadOld {
		if err := l.root.RemoveAll(old); err != nil {
			return err
		}
	}
	j.Phase = "complete"
	if err := l.saveJournal(j); err != nil {
		return err
	}
	return l.removeJournal(j.ID)
}

func cleanRelative(p string) (string, error) {
	if p == "" || strings.Contains(p, "\\") || strings.ContainsRune(p, 0) || filepath.IsAbs(p) {
		return "", ErrPath
	}
	p = filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	if p == "." || p == ".." || strings.HasPrefix(p, "../") || !fs.ValidPath(p) {
		return "", ErrPath
	}
	return p, nil
}

func rejectSymlinkComponents(root *os.Root, path string) error {
	parts := strings.Split(path, "/")
	for i := range parts {
		p := strings.Join(parts[:i+1], "/")
		info, err := root.Lstat(p)
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink component %s", ErrPath, p)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("%w: non-directory parent %s", ErrPath, p)
		}
	}
	return nil
}
