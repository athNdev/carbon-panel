// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package files

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriter(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "server.properties")
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := NewAtomicWriter(dest, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello")); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("overflow: %v", err)
	}
	if _, err := os.ReadFile(dest); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "old" {
		t.Fatalf("dest changed on overflow: %s", body)
	}
	if err := w.Abort(); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".atomic-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files left: %v", matches)
	}
	body, err = os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "old" {
		t.Fatalf("abort changed dest: %s", body)
	}

	w, err = NewAtomicWriter(dest, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); !errors.Is(err, ErrClosed) {
		t.Fatalf("second close: %v", err)
	}
	body, err = os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Fatalf("replaced body: %s", body)
	}
}
