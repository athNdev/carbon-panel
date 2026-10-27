// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package nodestorage routes server file, configuration and content I/O
// through the node that owns the server (ADR 0005). The local accessor is
// confined to the server data root. Remote nodes on shared storage are served
// through the controller's mount of that storage; other remote nodes get a
// typed refusal until a remote adapter is wired, so a controller-local path is
// never silently mistaken for a remote node's storage.
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

// NodeInfo is the storage-relevant view of a server's node. StorageRoot is
// the node-side root of the shared storage that holds server data directories;
// ControllerStorageRoot is where the controller mounts the same storage
// (empty when the paths are identical on both sides).
type NodeInfo struct {
	Name                  string
	IsLocal               bool
	StorageRoot           string
	ControllerStorageRoot string
}

// LocalNode returns the storage view of the controller's own daemon.
func LocalNode() NodeInfo {
	return NodeInfo{Name: "local", IsLocal: true}
}

// IsLegacyLocalNodeID reports whether a node ID designates the controller's
// own daemon on the legacy single-node deployment path. The aliases match the
// ones the Docker client pool accepts.
func IsLegacyLocalNodeID(nodeID string) bool {
	return nodeID == "" || nodeID == "local" || nodeID == "default"
}

// sharedStorageRoot returns the controller-side mount of a node's shared
// storage. The mount paths are identical on both sides when only the node-side
// root is configured.
func sharedStorageRoot(node NodeInfo) string {
	if node.ControllerStorageRoot != "" {
		return node.ControllerStorageRoot
	}
	return node.StorageRoot
}

// MapDataPath maps a server data root to the controller-side directory that
// holds it. Local nodes keep the path unchanged. Remote nodes on shared
// storage are mapped through the controller's mount of that storage: the
// server directory keeps its name under the controller-side root. Other
// remote nodes are refused with ErrUnavailable.
func MapDataPath(node NodeInfo, dataRoot string) (string, error) {
	if node.IsLocal {
		return dataRoot, nil
	}
	root := sharedStorageRoot(node)
	if root == "" {
		return "", fmt.Errorf("%w: %q", ErrUnavailable, node.Name)
	}
	return filepath.Join(root, filepath.Base(dataRoot)), nil
}

// relUnder returns the cleaned path of p relative to dataRoot. It reports
// false for paths outside the data root.
func relUnder(dataRoot, p string) (string, bool) {
	rel, err := filepath.Rel(dataRoot, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// MapPath maps an arbitrary path under a server data root to its
// controller-side location (see MapDataPath). Paths outside the data root and
// nodes without reachable storage are returned unchanged.
func MapPath(node NodeInfo, dataRoot, p string) string {
	if node.IsLocal {
		return p
	}
	root := sharedStorageRoot(node)
	if root == "" {
		return p
	}
	rel, ok := relUnder(dataRoot, p)
	if !ok {
		return p
	}
	return filepath.Join(root, filepath.Base(dataRoot), rel)
}

// NodePath maps an arbitrary path under a server data root to the node-side
// location that holds it: the Docker bind source for containers on that node.
// Local nodes and nodes without a declared storage root see identical paths
// on both sides, so the path is returned unchanged.
func NodePath(node NodeInfo, dataRoot, p string) string {
	if node.IsLocal || node.StorageRoot == "" {
		return p
	}
	rel, ok := relUnder(dataRoot, p)
	if !ok {
		return p
	}
	return filepath.Join(node.StorageRoot, filepath.Base(dataRoot), rel)
}

// EnsureDataRoot resolves the mapped data root like MapDataPath and creates
// the server directory under it when missing, so restore can target a fresh
// directory on shared storage. The controller-side storage root itself must
// already exist: creating it could hide a missing mount and silently store
// server data on the controller's local disk.
func EnsureDataRoot(node NodeInfo, dataRoot string) (string, error) {
	mapped, err := MapDataPath(node, dataRoot)
	if err != nil {
		return "", err
	}
	if !node.IsLocal {
		mount := sharedStorageRoot(node)
		if fi, serr := os.Stat(mount); serr != nil || !fi.IsDir() {
			return "", fmt.Errorf("%w: controller storage root %q for node %q is not accessible", ErrUnavailable, mount, node.Name)
		}
	}
	if err := os.MkdirAll(mapped, 0o755); err != nil {
		return "", fmt.Errorf("create data root for node %q: %w", node.Name, err)
	}
	return mapped, nil
}

// ForNode returns the accessor for a server data root owned by a node. Local
// nodes (the controller's own daemon) use the confined local accessor rooted
// at the server's data path. Remote nodes on shared storage are served through
// the controller's mount of that storage (see MapDataPath). Remote nodes
// without shared storage are refused until their storage adapter is wired.
func ForNode(node NodeInfo, dataRoot string) (Accessor, error) {
	if node.IsLocal {
		return NewLocal(dataRoot)
	}
	mapped, err := EnsureDataRoot(node, dataRoot)
	if err != nil {
		return nil, err
	}
	return NewLocal(mapped)
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
