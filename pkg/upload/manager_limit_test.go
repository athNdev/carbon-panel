// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package upload

import (
	"errors"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

// TestInitSessionEnforcesMaxSize covers the effective-limit wiring fixed in
// MINE-151: the manager must reject a session whose declared total exceeds the
// configured maximum, and must not impose a limit when the maximum is 0.
func TestInitSessionEnforcesMaxSize(t *testing.T) {
	log := logger.New()

	t.Run("limit enforced", func(t *testing.T) {
		m := NewManager(t.TempDir(), time.Hour, 1024, log)
		defer m.Stop()
		if _, err := m.InitSession("big.bin", 2048, 100); !errors.Is(err, ErrFileTooLarge) {
			t.Fatalf("expected ErrFileTooLarge for oversize session, got %v", err)
		}
		if _, err := m.InitSession("ok.bin", 512, 100); err != nil {
			t.Fatalf("expected within-limit session to be accepted, got %v", err)
		}
	})

	t.Run("zero means unlimited", func(t *testing.T) {
		m := NewManager(t.TempDir(), time.Hour, 0, log)
		defer m.Stop()
		if _, err := m.InitSession("huge.bin", 1<<20, 100); err != nil {
			t.Fatalf("expected no limit when maxUploadSize is 0, got %v", err)
		}
	})
}
