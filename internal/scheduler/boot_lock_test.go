// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package scheduler

import (
	"context"
	"strings"
	"testing"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/operation"
)

func TestStartServerForBootRefusesAnActiveDelete(t *testing.T) {
	s, server, _ := consistencyFixture(t)
	ctx := context.Background()
	if _, _, err := s.store.BeginServerOperation(ctx, server.ID, server.NodeID, server.ContainerID, "delete-key", "delete", operation.Digest("delete", server)); err != nil {
		t.Fatalf("BeginServerOperation: %v", err)
	}

	err := s.StartServerForBoot(ctx, server)
	if err == nil || !strings.Contains(err.Error(), "requires completion or recovery") {
		t.Fatalf("StartServerForBoot error = %v, want the delete lock to refuse boot", err)
	}
	if server.Status != storage.StatusRunning {
		t.Fatalf("boot changed status to %s", server.Status)
	}
}
