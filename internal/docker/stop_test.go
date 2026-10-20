// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package docker

import (
	"context"
	"errors"
	"testing"
	"time"

	models "github.com/athNdev/carbon-panel/internal/db"
)

func TestStopWindow(t *testing.T) {
	if got := StopWindow(nil); got != DefaultStopWindow {
		t.Fatalf("nil config window = %s, want %s", got, DefaultStopWindow)
	}
	zero := 0
	if got := StopWindow(&models.ServerConfig{StopDuration: &zero}); got != DefaultStopWindow {
		t.Fatalf("zero window = %s, want default", got)
	}
	twenty := 20
	if got := StopWindow(&models.ServerConfig{StopDuration: &twenty}); got != 20*time.Second {
		t.Fatalf("20s override = %s", got)
	}
	huge := 9999
	if got := StopWindow(&models.ServerConfig{StopDuration: &huge}); got != MaxStopWindow {
		t.Fatalf("cap = %s, want %s", got, MaxStopWindow)
	}
	if StopTimeoutSeconds(20*time.Second) != 20 {
		t.Fatal("20s window must stay 20s, long enough for a save that finishes at 20s")
	}
	if StopTimeoutSeconds(DefaultStopWindow) < 60 {
		t.Fatal("default docker timeout dropped below 60s")
	}
}

type recordingStop struct {
	gameStops int
	timeout   int
	kills     int
	stopErr   error
}

func (r *recordingStop) RequestGameStop(context.Context, string) error {
	r.gameStops++
	return errors.New("rcon down")
}

func (r *recordingStop) DockerStop(_ context.Context, _ string, timeoutSeconds int) error {
	r.timeout = timeoutSeconds
	return r.stopErr
}

func (r *recordingStop) ForceKill(context.Context, string) error {
	r.kills++
	return nil
}

func TestApplyStopLadderWaitsAndDoesNotKill(t *testing.T) {
	rec := &recordingStop{}
	if err := applyStopLadder(context.Background(), rec, "c1", 20*time.Second); err != nil {
		t.Fatal(err)
	}
	if rec.gameStops != 1 {
		t.Fatalf("game stop calls = %d, want 1", rec.gameStops)
	}
	if rec.timeout < 20 {
		t.Fatalf("docker timeout = %d, a 20s save would be killed", rec.timeout)
	}
	if rec.kills != 0 {
		t.Fatalf("kill calls = %d, want 0 when docker stop succeeds", rec.kills)
	}
}

func TestApplyStopLadderKillsOnlyAfterDockerStopFails(t *testing.T) {
	rec := &recordingStop{stopErr: errors.New("still running")}
	if err := applyStopLadder(context.Background(), rec, "c1", DefaultStopWindow); err != nil {
		t.Fatal(err)
	}
	if rec.timeout != 60 || rec.kills != 1 {
		t.Fatalf("timeout=%d kills=%d, want 60 and 1", rec.timeout, rec.kills)
	}
}

func TestApplyStopLadderMissingContainer(t *testing.T) {
	rec := &recordingStop{stopErr: errContainerGone}
	err := applyStopLadder(context.Background(), rec, "c1", DefaultStopWindow)
	if !errors.Is(err, errContainerGone) {
		t.Fatalf("err = %v", err)
	}
	if rec.kills != 0 {
		t.Fatal("missing container must not be killed")
	}
}
