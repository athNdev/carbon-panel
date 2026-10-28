// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/operation"
)

// runJournaledMutation persists the intent of a Docker mutation before it is
// attempted, advances the journal through the attempt, and finalizes it. See
// operation.RunJournaled: the per-server active lock is the shared
// coordinator that keeps concurrent lifecycle mutations of one world from
// interleaving (OSS-229).
func (s *ServerService) runJournaledMutation(ctx context.Context, server *storage.Server, kind, requestID string, mutate func() error) error {
	return operation.RunJournaled(ctx, s.store, s.log, server, kind, requestID, mutate)
}

func (s *ServerService) refuseActiveOperation(ctx context.Context, serverID string) error {
	if err := s.store.AssertNoServerOperation(ctx, serverID); err != nil {
		return connect.NewError(connect.CodeAborted, fmt.Errorf("refused while another operation owns the server: %w", err))
	}
	return nil
}

func lifecycleRequestID(raw string) (string, error) {
	if err := operation.ValidateRequestID(raw); err != nil {
		return "", err
	}
	return strings.TrimSpace(raw), nil
}
