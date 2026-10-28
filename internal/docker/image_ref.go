// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"fmt"
	"regexp"
	"strings"
)

// Server images stay on the two repositories this panel already ships.
// A tag with no repository is an itzg/minecraft-server tag. Any other
// repository is rejected so a blueprint or a pasted reference cannot
// retarget the game container.
const (
	minecraftServerRepo  = "itzg/minecraft-server"
	minecraftBedrockRepo = "itzg/minecraft-bedrock-server"
)

var (
	dockerTagPattern    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	dockerDigestPattern = regexp.MustCompile(`^sha256:[A-Fa-f0-9]{64}$`)
)

type serverImage struct {
	repo   string
	tag    string
	digest string
}

func (s serverImage) reference() string {
	out := s.repo
	if s.tag != "" {
		out += ":" + s.tag
	}
	if s.digest != "" {
		out += "@" + s.digest
	}
	return out
}

// stored keeps a Java server image as the tag the image picker already
// saves. Bedrock and digest pins keep the full reference, because a bare
// tag would point at itzg/minecraft-server.
func (s serverImage) stored() string {
	if s.repo == minecraftServerRepo && s.digest == "" {
		return s.tag
	}
	return s.reference()
}

// ResolveServerImage returns the daemon reference for a stored server image.
func ResolveServerImage(dockerImage string) (string, error) {
	parsed, err := parseServerImage(dockerImage)
	if err != nil {
		return "", err
	}
	return parsed.reference(), nil
}

// CanonicalServerImage returns the value to persist. Bare itzg/minecraft-server
// tags stay tags. Allowlisted full references are normalized. Anything else
// is rejected.
func CanonicalServerImage(dockerImage string) (string, error) {
	parsed, err := parseServerImage(dockerImage)
	if err != nil {
		return "", err
	}
	return parsed.stored(), nil
}

func parseServerImage(raw string) (serverImage, error) {
	original := raw
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, " \t\r\n\\") {
		return serverImage{}, fmt.Errorf("invalid server image reference %q", original)
	}
	ref := raw
	if strings.HasPrefix(strings.ToLower(ref), "docker.io/") {
		ref = ref[len("docker.io/"):]
	}
	if slash := strings.IndexByte(ref, '/'); slash >= 0 && strings.Contains(ref[:slash], ":") {
		return serverImage{}, fmt.Errorf("server image %q is not an allowed Minecraft server image", original)
	}

	digest := ""
	if at := strings.LastIndex(ref, "@"); at >= 0 {
		digest = ref[at+1:]
		ref = ref[:at]
		if !dockerDigestPattern.MatchString(digest) {
			return serverImage{}, fmt.Errorf("invalid server image reference %q", original)
		}
	}

	if !strings.Contains(ref, "/") {
		if digest != "" || !dockerTagPattern.MatchString(ref) {
			return serverImage{}, fmt.Errorf("invalid server image reference %q", original)
		}
		return serverImage{repo: minecraftServerRepo, tag: ref}, nil
	}

	repo := ref
	tag := ""
	if colon := strings.LastIndex(ref, ":"); colon >= 0 {
		repo = ref[:colon]
		tag = ref[colon+1:]
		if !dockerTagPattern.MatchString(tag) {
			return serverImage{}, fmt.Errorf("invalid server image reference %q", original)
		}
	}
	canonical, ok := canonicalServerRepo(repo)
	if !ok {
		return serverImage{}, fmt.Errorf("server image %q is not an allowed Minecraft server image", original)
	}
	if tag == "" && digest == "" {
		tag = "latest"
	}
	return serverImage{repo: canonical, tag: tag, digest: digest}, nil
}

func canonicalServerRepo(repo string) (string, bool) {
	switch strings.ToLower(repo) {
	case minecraftServerRepo, minecraftBedrockRepo:
		return strings.ToLower(repo), true
	default:
		return "", false
	}
}
