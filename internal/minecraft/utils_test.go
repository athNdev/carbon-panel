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

func TestParseTPSFromOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   float64
	}{
		{"overall priority", "Overall: 20.000 TPS\nTPS from last 1m, 5m, 15m: 19.5, 19.5, 19.5", 20.0},
		{"vanilla tps from last", "TPS from last 1m, 5m, 15m: 20.0, 20.0, 20.0", 20.0},
		{"forge mean tps", "Dim 0 (overworld): Mean tick time: 2.333 ms. Mean TPS: 17.5", 17.5},
		{"overworld format", "Overworld: 19.200 TPS", 19.2},
		{"generic lowercase", "tps: 20.0", 20.0},
		{"current tps", "Current TPS: 19.8", 19.8},
		{"color stripped", "\u00c2\u00a7aOverall: 18.500 TPS", 18.5},
		{"last resort bare number", "18", 18},
		{"no numbers", "nothing here", 0},
		{"out of range rejected", "25", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseTPSFromOutput(tt.output); got != tt.want {
				t.Errorf("ParseTPSFromOutput(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestParsePlayerListFromOutput(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantCount int
		wantNames []string
	}{
		{"vanilla list", "There are 2 of a max 20 players online: alice, bob", 2, []string{"alice", "bob"}},
		{"slash format", "3/20 players online: x, y, z", 3, []string{"x", "y", "z"}},
		{"nobody online", "0 of a max 20 players online: None", 0, []string{}},
		{"count without names", "There are 4 of a max 10 players online", 4, []string{}},
		{"players in parens", "Players online (5): x, y", 2, []string{"x", "y"}},
		{"empty output", "", 0, []string{}},
		{"color stripped", "\u00c2\u00a7aThere are 1 of a max 20 players online: steve", 1, []string{"steve"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCount, gotNames := ParsePlayerListFromOutput(tt.output)
			if gotCount != tt.wantCount {
				t.Errorf("ParsePlayerListFromOutput(%q) count = %d, want %d", tt.output, gotCount, tt.wantCount)
			}
			if !reflect.DeepEqual(gotNames, tt.wantNames) {
				t.Errorf("ParsePlayerListFromOutput(%q) names = %v, want %v", tt.output, gotNames, tt.wantNames)
			}
		})
	}
}

func TestStripMinecraftColors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"color code", "\u00c2\u00a7aHello", "Hello"},
		{"stacked codes", "\u00c2\u00a7l\u00c2\u00a7cBold", "Bold"},
		{"ansi escapes", "\x1b[31mRed\x1b[0m", "Red"},
		{"plain text", "Hello", "Hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripMinecraftColors(tt.in); got != tt.want {
				t.Errorf("stripMinecraftColors(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
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
