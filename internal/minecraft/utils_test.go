// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package minecraft

import (
	"reflect"
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
)

func TestParseTPSFromOutputDelegates(t *testing.T) {
	if got := ParseTPSFromOutput("Overall: 20.000 TPS"); got != 20 {
		t.Errorf("ParseTPSFromOutput() = %v, want 20", got)
	}
}

func TestParsePlayerListFromOutputDelegates(t *testing.T) {
	gotCount, gotNames := ParsePlayerListFromOutput("There are 2 of a max 20 players online: alice, bob")
	if gotCount != 2 {
		t.Errorf("count = %d, want 2", gotCount)
	}
	if !reflect.DeepEqual(gotNames, []string{"alice", "bob"}) {
		t.Errorf("names = %v", gotNames)
	}
}

func TestGetTPSCommand(t *testing.T) {
	tests := []struct {
		loader models.ModLoader
		want   string
	}{
		{models.ModLoaderPaper, "tps"},
		{models.ModLoaderFolia, "tps"},
		{models.ModLoaderPurpur, "tps"},
		{models.ModLoaderSpigot, "tps"},
		{models.ModLoaderBukkit, "tps"},
		{models.ModLoaderPufferfish, "tps"},
		{models.ModLoaderMagma, "tps"},
		{models.ModLoaderMagmaMaintained, "tps"},
		{models.ModLoaderMohist, "tps"},
		{models.ModLoaderArclight, "tps"},
		{models.ModLoaderForge, "forge tps"},
		{models.ModLoaderNeoForge, "neoforge tps"},
		{models.ModLoaderVanilla, ""},
		{models.ModLoaderFabric, "forge tps ?? neoforge tps ?? tps"},
		{models.ModLoaderCustom, "forge tps ?? neoforge tps ?? tps"},
	}
	for _, tt := range tests {
		t.Run(string(tt.loader), func(t *testing.T) {
			if got := GetTPSCommand(tt.loader); got != tt.want {
				t.Errorf("GetTPSCommand(%q) = %q, want %q", tt.loader, got, tt.want)
			}
		})
	}
}
