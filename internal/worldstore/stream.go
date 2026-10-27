// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package worldstore

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Streaming variants keep memory flat for large worlds while preserving the
// same manifest format and safety rules as the in-memory API. Their limits are
// higher because they protect real world backups (region files are large) and
// every extracted byte is still verified against the manifest.
const (
	streamMaxFileBytes  = 4 << 30
	streamMaxWorldBytes = 64 << 30
	streamMaxEntries    = 500_000
)

const manifestName = ".worldstore-manifest.json"

type archiveManifest struct {
	Version int     `json:"version"`
	Files   []Entry `json:"files"`
}

// HasWorldstoreManifest reports whether a zip archive carries the worldstore
// manifest, i.e. was produced by Backup/BackupTo and belongs on the atomic
// restore path.
func HasWorldstoreManifest(path string) bool {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if strings.TrimSuffix(f.Name, "/") == manifestName {
			return true
		}
	}
	return false
}

// zipArchive is a verified-on-open worldstore archive. Open validates the
// whole archive shape and manifest coverage; extract verifies content hashes
// while streaming each file into place.
type zipArchive struct {
	zr       *zip.ReadCloser
	byPath   map[string]*zip.File
	manifest map[string]Entry
}

// openZipArchive validates names, modes, sizes, entry counts and manifest
// coverage without expanding file contents.
func openZipArchive(path string) (*zipArchive, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	sx := &zipArchive{zr: zr, byPath: map[string]*zip.File{}}
	fail := func(err error) (*zipArchive, error) {
		_ = zr.Close()
		return nil, err
	}

	var manifestBytes []byte
	if len(zr.File) > streamMaxEntries+1 {
		return fail(errors.New("worldstore: archive entry count outside limits"))
	}
	for _, zf := range zr.File {
		name := strings.TrimSuffix(zf.Name, "/")
		if name == manifestName {
			if manifestBytes != nil {
				return fail(errors.New("worldstore: duplicate manifest"))
			}
			if !zf.Mode().IsRegular() || zf.Mode()&os.ModeSymlink != 0 {
				return fail(errors.New("worldstore: invalid manifest entry"))
			}
			if zf.UncompressedSize64 > 1<<20 {
				return fail(errors.New("worldstore: manifest too large"))
			}
			f, err := zf.Open()
			if err != nil {
				return fail(err)
			}
			manifestBytes, err = io.ReadAll(io.LimitReader(f, 1<<20+1))
			ce := f.Close()
			if err != nil {
				return fail(err)
			}
			if ce != nil {
				return fail(ce)
			}
			continue
		}
		p, err := cleanRelative(name)
		if err != nil || p != name {
			return fail(fmt.Errorf("%w: archive entry %q", ErrPath, zf.Name))
		}
		if strings.HasSuffix(zf.Name, "/") {
			if !zf.Mode().IsDir() {
				return fail(fmt.Errorf("worldstore: invalid directory entry %q", zf.Name))
			}
			continue
		}
		if _, ok := sx.byPath[p]; ok {
			return fail(fmt.Errorf("worldstore: duplicate archive entry %q", p))
		}
		if zf.Mode()&os.ModeSymlink != 0 || !zf.Mode().IsRegular() {
			return fail(fmt.Errorf("worldstore: non-regular archive entry %q", p))
		}
		if zf.UncompressedSize64 > uint64(streamMaxFileBytes) {
			return fail(fmt.Errorf("worldstore: file too large: %s", p))
		}
		sx.byPath[p] = zf
	}
	if manifestBytes == nil {
		return fail(errors.New("worldstore: backup manifest missing"))
	}
	var manifest archiveManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fail(err)
	}
	if manifest.Version != 1 {
		return fail(fmt.Errorf("worldstore: unsupported manifest version %d", manifest.Version))
	}
	sx.manifest = make(map[string]Entry, len(manifest.Files))
	for _, e := range manifest.Files {
		p, err := cleanRelative(e.Path)
		if err != nil || p != e.Path {
			return fail(fmt.Errorf("%w: manifest entry %q", ErrPath, e.Path))
		}
		if _, ok := sx.manifest[p]; ok {
			return fail(fmt.Errorf("worldstore: duplicate manifest entry %q", p))
		}
		if e.Size < 0 || e.Size > streamMaxFileBytes {
			return fail(fmt.Errorf("worldstore: manifest size out of bounds: %s", p))
		}
		sx.manifest[p] = e
	}
	// Coverage: the archive and the manifest must describe the same file set.
	if len(sx.manifest) != len(sx.byPath) {
		return fail(errors.New("worldstore: manifest does not match archive contents"))
	}
	for p := range sx.byPath {
		if _, ok := sx.manifest[p]; !ok {
			return fail(fmt.Errorf("worldstore: entry %q missing from manifest", p))
		}
	}
	return sx, nil
}

func (sx *zipArchive) Close() error { return sx.zr.Close() }

// extract streams every entry into the stage tree under root, verifying each
// file's size and SHA-256 against the manifest as it writes.
func (sx *zipArchive) extract(root *os.Root, stage string) error {
	paths := make([]string, 0, len(sx.byPath))
	for p := range sx.byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var total int64
	for _, p := range paths {
		zf := sx.byPath[p]
		want := sx.manifest[p]
		dest := filepath.ToSlash(filepath.Join(stage, p))
		if err := root.MkdirAll(filepath.ToSlash(filepath.Dir(dest)), 0o700); err != nil {
			return err
		}
		out, err := root.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		src, err := zf.Open()
		if err != nil {
			_ = out.Close()
			return err
		}
		hash := sha256.New()
		n, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(src, streamMaxFileBytes+1))
		cerr := out.Close()
		serr := src.Close()
		if err != nil {
			return err
		}
		if cerr != nil {
			return cerr
		}
		if serr != nil {
			return serr
		}
		if n != want.Size {
			return fmt.Errorf("worldstore: size mismatch for %s: manifest %d, archive %d", p, want.Size, n)
		}
		if got := hex.EncodeToString(hash.Sum(nil)); got != want.SHA256 {
			return fmt.Errorf("worldstore: hash mismatch for %s", p)
		}
		total += n
		if total > streamMaxWorldBytes {
			return errors.New("worldstore: expanded archive exceeds streaming limit")
		}
	}
	return nil
}
