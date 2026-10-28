// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package staging

import (
	"time"

	storage "github.com/athNdev/carbon-panel/internal/db"
)

// PlayerSample is one reading from the same player-count provider freeze and
// deep sleep use. Known is false when that provider is not installed.
type PlayerSample struct {
	Known bool
	Count int
}

// Decision is what a sweep should do with one open batch. SetIdle persists
// IdleSince, including a nil value that clears a stretch after a player joins.
type Decision struct {
	Due       bool
	SetIdle   bool
	IdleSince *time.Time
}

// Decide reports whether a batch should be published now. A next-restart batch
// is never due here: an incidental start must not publish a future time or an
// idle window, and a next-restart batch waits for that explicit restart.
// A missing player provider does not start or fire an idle window.
func Decide(now time.Time, batch storage.ServerUpdateBatch, players PlayerSample) Decision {
	switch batch.Window {
	case string(storage.UpdateWindowAtTime):
		if batch.ApplyAt != nil && !now.Before(*batch.ApplyAt) {
			return Decision{Due: true}
		}
	case string(storage.UpdateWindowIdle):
		if !players.Known || batch.IdleMinutes <= 0 {
			return Decision{}
		}
		if players.Count > 0 {
			if batch.IdleSince != nil {
				return Decision{SetIdle: true}
			}
			return Decision{}
		}
		if batch.IdleSince == nil {
			since := now.UTC()
			return Decision{SetIdle: true, IdleSince: &since}
		}
		if now.Sub(*batch.IdleSince) >= time.Duration(batch.IdleMinutes)*time.Minute {
			return Decision{Due: true}
		}
	}
	return Decision{}
}

// Plan is the container work that publishes a batch after its files and config
// are on the server. Deep sleep commits onto the stopped server and does not
// start the JVM. A frozen server gets a real stop and start so the new process
// is the one left running.
type Plan struct {
	SaveAll    bool
	Restart    bool
	Recreate   bool
	StartAfter bool
}

// PlanFor chooses the container action for a server status. configChanged
// means the batch merged ServerConfig, so the container spec has to be rebuilt.
func PlanFor(status storage.ServerStatus, configChanged bool) Plan {
	switch status {
	case storage.StatusRunning:
		return Plan{SaveAll: true, Restart: !configChanged, Recreate: configChanged, StartAfter: true}
	case storage.StatusPaused:
		return Plan{Restart: !configChanged, Recreate: configChanged, StartAfter: true}
	case storage.StatusDeepSleep:
		return Plan{Recreate: configChanged}
	default:
		if configChanged {
			return Plan{Recreate: true}
		}
		return Plan{}
	}
}

// TouchesContainer reports whether publishing the plan stops, starts, or
// replaces a container.
func (p Plan) TouchesContainer() bool {
	return p.Restart || p.Recreate || p.StartAfter
}
