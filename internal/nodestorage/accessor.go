// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package nodestorage routes server file, configuration and content I/O
// through the node that owns the server (ADR 0005). The local accessor is
// confined to the server data root; remote nodes get a typed refusal until a
// remote adapter is wired, so a controller-local path is never silently
// mistaken for a remote node's storage.
package nodestorage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnavailable reports that the owning node's storage adapter is not
// configured. Callers must treat it as a refusal, never fall back to local
// paths for a remote node.
var ErrUnavailable = errors.New("node-aware storage is not configured for this node")

// ErrEscape reports a path that would leave the server data root.
var ErrEscape = errors.New("path escapes the data root")

// ErrSymlink reports an attempt to write through a symbolic link. Writes must
// land on real files inside the data root.
var ErrSymlink = errors.New("cannot write through symbolic link")

// Accessor performs file operations inside one server's data root on the node
// that owns the server. All paths are relative to the root; every operation is
// confined to it.
type Accessor interface {
	Stat(rel string) (os.FileInfo, error)
	ReadFile(rel string) ([]byte, error)
	WriteFile(rel string, data []byte) error
	ReadDir(rel string) ([]os.DirEntry, error)
	MkdirAll(rel string) error
	Remove(rel string) error
	RemoveAll(rel string) error
	Rename(oldRel, newRel string) error
	Root() string
	// Close releases the accessor's handle on the data root. Callers must
	// close accessors they open.
	Close() error
}

// local confines operations to a server data root using os.Root, so no
// relative or absolute path can escape it on any platform.
type local struct {
	root *os.Root
	dir  string
}

// NewLocal returns the accessor for a server data root on this controller.
func NewLocal(dataRoot string) (Accessor, error) {
	abs, err := filepath.Abs(dataRoot)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(filepath.Dir(abs))
	if err != nil {
		return nil, fmt.Errorf("open storage root: %w", err)
	}
	return &local{root: r, dir: filepath.Base(abs)}, nil
}

func (l *local) Root() string { return filepath.Join(l.root.Name(), l.dir) }

func (l *local) Close() error { return l.root.Close() }

// refuseSymlink preserves the file service's content-safety rule: never write
// through a symbolic link.
func (l *local) refuseSymlink(p string) error {
	if fi, err := l.root.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %q", ErrSymlink, p)
	}
	return nil
}

// sub resolves a caller path under the data root and rejects anything that
// would escape it. Absolute paths are never legitimate root-relative inputs
// and are rejected outright; traversal segments are cleaned and confined.
func (l *local) sub(rel string) (string, error) {
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || filepath.VolumeName(rel) != "" {
		return "", fmt.Errorf("%w: %q", ErrEscape, rel)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.Join(l.dir, filepath.FromSlash(rel))))
	if clean != l.dir && (len(clean) <= len(l.dir)+1 || clean[:len(l.dir)+1] != l.dir+"/") {
		return "", fmt.Errorf("%w: %q", ErrEscape, rel)
	}
	return clean, nil
}

func (l *local) Stat(rel string) (os.FileInfo, error) {
	p, err := l.sub(rel)
	if err != nil {
		return nil, err
	}
	return l.root.Stat(p)
}

func (l *local) ReadFile(rel string) ([]byte, error) {
	p, err := l.sub(rel)
	if err != nil {
		return nil, err
	}
	return l.root.ReadFile(p)
}

func (l *local) WriteFile(rel string, data []byte) error {
	p, err := l.sub(rel)
	if err != nil {
		return err
	}
	if err := l.refuseSymlink(p); err != nil {
		return err
	}
	if err := l.root.MkdirAll(filepath.ToSlash(filepath.Dir(p)), 0o755); err != nil {
		return err
	}
	return l.root.WriteFile(p, data, 0o644)
}

func (l *local) ReadDir(rel string) ([]os.DirEntry, error) {
	p, err := l.sub(rel)
	if err != nil {
		return nil, err
	}
	// os.Root has no ReadDir; open the directory through the root and read it.
	f, err := l.root.Open(p)
	if err != nil {
		return nil, err
	}
	entries, err := f.ReadDir(-1)
	_ = f.Close()
	return entries, err
}

func (l *local) MkdirAll(rel string) error {
	p, err := l.sub(rel)
	if err != nil {
		return err
	}
	return l.root.MkdirAll(p, 0o755)
}

func (l *local) Remove(rel string) error {
	p, err := l.sub(rel)
	if err != nil {
		return err
	}
	return l.root.Remove(p)
}

func (l *local) RemoveAll(rel string) error {
	p, err := l.sub(rel)
	if err != nil {
		return err
	}
	return l.root.RemoveAll(p)
}

func (l *local) Rename(oldRel, newRel string) error {
	o, err := l.sub(oldRel)
	if err != nil {
		return err
	}
	n, err := l.sub(newRel)
	if err != nil {
		return err
	}
	if err := l.refuseSymlink(n); err != nil {
		return err
	}
	if err := l.root.MkdirAll(filepath.ToSlash(filepath.Dir(n)), 0o755); err != nil {
		return err
	}
	return l.root.Rename(o, n)
}

// NodeInfo is the storage-relevant view of a server's node.
type NodeInfo struct {
	Name    string
	IsLocal bool
}

// ForNode returns the accessor for a server data root owned by a node. Local
// nodes (the controller's own daemon) use the confined local accessor; remote
// nodes are refused until their storage adapter is wired.
func ForNode(node NodeInfo, dataRoot string) (Accessor, error) {
	if node.IsLocal {
		return NewLocal(dataRoot)
	}
	return nil, fmt.Errorf("%w: %q", ErrUnavailable, node.Name)
}

// IsUnavailable reports whether err is the typed remote-node refusal.
func IsUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}

// IsEscape reports whether err is a path-escape rejection.
func IsEscape(err error) bool {
	return errors.Is(err, ErrEscape)
}

// IsSymlink reports whether err is a symlink-write refusal.
func IsSymlink(err error) bool {
	return errors.Is(err, ErrSymlink)
}
