// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.


package minecraft

import (
	models "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/mcstatus"
)

// ParseTPSFromOutput parses TPS value from various server command outputs.
func ParseTPSFromOutput(output string) float64 {
	return mcstatus.ParseTPSFromOutput(output)
}

// ParsePlayerListFromOutput parses player count and names from list command output.
func ParsePlayerListFromOutput(output string) (int, []string) {
	return mcstatus.ParsePlayerListFromOutput(output)
}

func GetTPSCommand(modLoader models.ModLoader) string {
	switch modLoader {
	case models.ModLoaderPaper, models.ModLoaderFolia, models.ModLoaderPurpur, models.ModLoaderSpigot, models.ModLoaderBukkit, models.ModLoaderPufferfish:
		return "tps"
	case models.ModLoaderForge:
		return "forge tps"
	case models.ModLoaderNeoForge:
		return "neoforge tps"
	case models.ModLoaderMagma, models.ModLoaderMagmaMaintained, models.ModLoaderMohist, models.ModLoaderArclight:
		return "tps"
	case models.ModLoaderVanilla:
		return ""
	default:
		return "forge tps ?? neoforge tps ?? tps"
	}
}
