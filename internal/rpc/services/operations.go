// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/operation"
)

// runJournaledMutation persists the intent of a Docker mutation before it is
// attempted, advances the journal through the attempt, and finalizes it. See
// operation.RunJournaled: the per-server active lock is the shared
// coordinator that keeps concurrent lifecycle mutations of one world from
// interleaving (OSS-229).
func (s *ServerService) runJournaledMutation(ctx context.Context, server *storage.Server, kind string, mutate func() error) error {
	return operation.RunJournaled(ctx, s.store, s.log, server, kind, mutate)
}
