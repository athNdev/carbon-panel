// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package mcstatus

import (
	"regexp"
	"strconv"
	"strings"
)

// ParseTPSFromOutput parses a TPS value from Minecraft server command output.
func ParseTPSFromOutput(output string) float64 {
	output = stripMinecraftColors(output)

	patterns := []struct {
		regex   *regexp.Regexp
		extract func([]string) float64
	}{
		{
			regex: regexp.MustCompile(`Overall:\s*([\d.]+)\s*TPS`),
			extract: func(matches []string) float64 {
				if len(matches) > 1 {
					val, _ := strconv.ParseFloat(matches[1], 64)
					return val
				}
				return 0
			},
		},
		{
			regex: regexp.MustCompile(`TPS from last .*?:\s*([\d.]+)`),
			extract: func(matches []string) float64 {
				if len(matches) > 1 {
					val, _ := strconv.ParseFloat(matches[1], 64)
					return val
				}
				return 0
			},
		},
		{
			regex: regexp.MustCompile(`Mean TPS:\s*([\d.]+)`),
			extract: func(matches []string) float64 {
				if len(matches) > 1 {
					val, _ := strconv.ParseFloat(matches[1], 64)
					return val
				}
				return 0
			},
		},
		{
			regex: regexp.MustCompile(`Overworld:\s*([\d.]+)\s*TPS`),
			extract: func(matches []string) float64 {
				if len(matches) > 1 {
					val, _ := strconv.ParseFloat(matches[1], 64)
					return val
				}
				return 0
			},
		},
		{
			regex: regexp.MustCompile(`(?i)tps[\s:]+(\d+\.?\d*)`),
			extract: func(matches []string) float64 {
				if len(matches) > 1 {
					val, _ := strconv.ParseFloat(matches[1], 64)
					return val
				}
				return 0
			},
		},
		{
			regex: regexp.MustCompile(`\b(\d{1,2}\.?\d*)\b`),
			extract: func(matches []string) float64 {
				if len(matches) > 1 {
					val, _ := strconv.ParseFloat(matches[1], 64)
					if val > 0 && val <= 20 {
						return val
					}
				}
				return 0
			},
		},
	}

	for _, pattern := range patterns {
		matches := pattern.regex.FindStringSubmatch(output)
		if tps := pattern.extract(matches); tps > 0 {
			return tps
		}
	}

	return 0
}

// ParsePlayerListFromOutput parses player count and names from list command output.
func ParsePlayerListFromOutput(output string) (int, []string) {
	output = stripMinecraftColors(output)

	var count int
	var players []string

	re := regexp.MustCompile(`(\d+)\s*(?:of|/)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) > 1 {
		count, _ = strconv.Atoi(matches[1])
	}

	colonIdx := strings.Index(output, ":")
	if colonIdx != -1 && colonIdx < len(output)-1 {
		playersPart := output[colonIdx+1:]
		names := strings.SplitSeq(playersPart, ",")
		for name := range names {
			cleaned := strings.TrimSpace(name)
			if cleaned != "" && cleaned != "None" {
				players = append(players, cleaned)
			}
		}
	}

	if count > 0 && len(players) == 0 {
		return count, []string{}
	}

	if len(players) > 0 {
		return len(players), players
	}

	return 0, []string{}
}

func stripMinecraftColors(text string) string {
	re := regexp.MustCompile(`Â§.`)
	text = re.ReplaceAllString(text, "")

	ansiRe := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	text = ansiRe.ReplaceAllString(text, "")

	return text
}
