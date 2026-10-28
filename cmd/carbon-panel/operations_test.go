// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
)

func TestOperationsListAndResolveDoNotRequireDocker(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:        filepath.Join(t.TempDir(), "ops.db"),
			AutoMigrate: true,
		},
		Docker: config.DockerConfig{Host: "unix:///var/run/docker.sock"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	op, _, err := store.BeginServerOperation(ctx, "srv-1", "node-1", "cont-1", "key-1", "start", "digest")
	if err != nil {
		t.Fatal(err)
	}

	var listed bytes.Buffer
	if err := runOperationsCommand(ctx, store, "list", "", "", &listed); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.String(), op.ID) || !strings.Contains(listed.String(), "\tyes\t") {
		t.Fatalf("list = %q", listed.String())
	}

	var resolved bytes.Buffer
	if err := runOperationsCommand(ctx, store, "resolve", op.ID, "checked the container by hand", &resolved); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resolved.String(), "without changing containers") {
		t.Fatalf("resolve = %q", resolved.String())
	}
	if err := store.AssertNoServerOperation(ctx, "srv-1"); err != nil {
		t.Fatal(err)
	}

	var again bytes.Buffer
	if err := runOperationsCommand(ctx, store, "resolve", op.ID, "", &again); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(again.String(), "already released") {
		t.Fatalf("second resolve = %q", again.String())
	}
	if err := runOperationsCommand(ctx, store, "resolve", "", "", &bytes.Buffer{}); err == nil {
		t.Fatal("expected resolve without an id to fail")
	}
}
