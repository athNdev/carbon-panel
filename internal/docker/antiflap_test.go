// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package docker

import (
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/db"
)

// TestBuildEnvFromConfig_LegacyAutoTimersGatedByNativePause verifies MINE-122:
// when native pause-when-empty is engaged, the itzg autopause/autostop timers
// are forced off so one layer owns idleness (they fight the panel lifecycle:
// knockd wakes on any traffic, autostop exits get resurrected).
func TestBuildEnvFromConfig_LegacyAutoTimersGatedByNativePause(t *testing.T) {
	trueVal := true
	pause := 60
	cfg := &db.ServerConfig{
		EnableAutopause:       &trueVal,
		EnableAutostop:        &trueVal,
		PauseWhenEmptySeconds: &pause,
	}

	env := buildEnvFromConfig(cfg)

	got := map[string]string{}
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok {
			if k == "ENABLE_AUTOPAUSE" || k == "ENABLE_AUTOSTOP" || k == "PAUSE_WHEN_EMPTY_SECONDS" {
				got[k] = v
			}
		}
	}

	if got["PAUSE_WHEN_EMPTY_SECONDS"] != "60" {
		t.Fatalf("expected PAUSE_WHEN_EMPTY_SECONDS=60, got %q", got["PAUSE_WHEN_EMPTY_SECONDS"])
	}
	if got["ENABLE_AUTOPAUSE"] != "false" {
		t.Fatalf("expected ENABLE_AUTOPAUSE=false when native pause engaged, got %q", got["ENABLE_AUTOPAUSE"])
	}
	if got["ENABLE_AUTOSTOP"] != "false" {
		t.Fatalf("expected ENABLE_AUTOSTOP=false when native pause engaged, got %q", got["ENABLE_AUTOSTOP"])
	}
}

// TestBuildEnvFromConfig_LegacyAutoTimersPreservedWithoutNativePause verifies
// operators who explicitly disable native pause keep the legacy itzg layer.
func TestBuildEnvFromConfig_LegacyAutoTimersPreservedWithoutNativePause(t *testing.T) {
	trueVal := true
	cfg := &db.ServerConfig{
		EnableAutopause: &trueVal,
		EnableAutostop:  &trueVal,
		// PauseWhenEmptySeconds nil: no native pause, legacy layer untouched.
	}

	env := buildEnvFromConfig(cfg)

	got := map[string]string{}
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok {
			if k == "ENABLE_AUTOPAUSE" || k == "ENABLE_AUTOSTOP" {
				got[k] = v
			}
		}
	}

	if got["ENABLE_AUTOPAUSE"] != "true" {
		t.Fatalf("expected ENABLE_AUTOPAUSE=true preserved without native pause, got %q", got["ENABLE_AUTOPAUSE"])
	}
	if got["ENABLE_AUTOSTOP"] != "true" {
		t.Fatalf("expected ENABLE_AUTOSTOP=true preserved without native pause, got %q", got["ENABLE_AUTOSTOP"])
	}
}
