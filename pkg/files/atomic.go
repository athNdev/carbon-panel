// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrFileTooLarge is returned when a write would pass the declared size.
// ErrClosed is returned by a second Close or by Write after Close or Abort.
var (
	ErrFileTooLarge = errors.New("files: file exceeds declared size")
	ErrClosed       = errors.New("files: writer closed")
)

// AtomicWriter writes to a temp file beside dest and renames it into place
// on Close. The caller resolves dest, including ResolveInRoot, before calling
// NewAtomicWriter. Abort removes the temp file and leaves dest unchanged.
type AtomicWriter struct {
	dest     string
	tmp      *os.File
	maxBytes int64
	written  int64
	done     bool
}

// NewAtomicWriter creates a temp file in the destination directory.
// maxBytes is the most Write will accept. The destination directory must exist.
func NewAtomicWriter(dest string, maxBytes int64) (*AtomicWriter, error) {
	if dest == "" {
		return nil, ErrEmptyRoot
	}
	if maxBytes < 0 {
		return nil, fmt.Errorf("files: negative size")
	}
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".atomic-*")
	if err != nil {
		return nil, err
	}
	return &AtomicWriter{dest: dest, tmp: tmp, maxBytes: maxBytes}, nil
}

// Write rejects a chunk that would pass maxBytes and writes none of that chunk.
func (w *AtomicWriter) Write(p []byte) (int, error) {
	if w.done {
		return 0, ErrClosed
	}
	if w.written+int64(len(p)) > w.maxBytes {
		return 0, ErrFileTooLarge
	}
	n, err := w.tmp.Write(p)
	w.written += int64(n)
	if err != nil {
		return n, err
	}
	if n != len(p) {
		return n, fmt.Errorf("files: short write")
	}
	return n, nil
}

// Abort removes the temp file. It does not remove a destination already published by Close.
func (w *AtomicWriter) Abort() error {
	if w.done {
		return ErrClosed
	}
	w.done = true
	name := w.tmp.Name()
	closeErr := w.tmp.Close()
	removeErr := os.Remove(name)
	return errors.Join(closeErr, removeErr)
}

// Close publishes the temp file as dest, replacing an existing file.
func (w *AtomicWriter) Close() error {
	if w.done {
		return ErrClosed
	}
	w.done = true
	if err := w.tmp.Sync(); err != nil {
		_ = w.tmp.Close()
		_ = os.Remove(w.tmp.Name())
		return err
	}
	name := w.tmp.Name()
	if err := w.tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := replaceFile(name, w.dest); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func replaceFile(tmp, dest string) error {
	backup := dest + ".replace"
	_ = os.Remove(backup)
	moved := false
	if _, err := os.Lstat(dest); err == nil {
		if err := os.Rename(dest, backup); err != nil {
			return err
		}
		moved = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		if moved {
			_ = os.Rename(backup, dest)
		}
		return err
	}
	if moved {
		_ = os.Remove(backup)
	}
	return nil
}
