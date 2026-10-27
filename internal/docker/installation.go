// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/moby/moby/client"
)

const (
	// LabelInstallation records which controller installation created a container.
	// Startup reconciliation only adopts or purges containers carrying this
	// installation's identity (or legacy containers that match a live DB record).
	LabelInstallation = "carbon-panel.installation"

	installationIDFileName = "installation-id"
)

// installationIDPattern accepts UUID-shaped identities. It is deliberately strict:
// a malformed identity file must fail startup instead of silently generating a new
// identity, which would make every container created by this installation look
// foreign and permanently strand them.
var installationIDPattern = regexp.MustCompile(`^[0-9a-fA-F][0-9a-fA-F-]{15,63}$`)

// LoadOrCreateInstallationID returns the stable identity of this controller
// installation, creating it once under dataDir if it does not exist yet.
//
// The identity lives next to the panel database so that upgrading, restarting or
// restoring an older database of the same installation keeps the same ownership
// scope. A second panel installation sharing a Docker daemon has a different data
// directory and therefore a different identity.
func LoadOrCreateInstallationID(dataDir string) (string, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return "", errors.New("installation identity: data directory is empty")
	}
	path := filepath.Join(dataDir, installationIDFileName)

	data, err := os.ReadFile(path)
	if err == nil {
		id := strings.TrimSpace(string(data))
		if !installationIDPattern.MatchString(id) {
			return "", fmt.Errorf("installation identity: %s is malformed; restore the original file to keep ownership of existing containers, or remove it deliberately to start with a new identity", path)
		}
		return id, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("installation identity: read %s: %w", path, err)
	}

	id := uuid.NewString()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			// Another process won the first-start race; use its identity.
			if data, readErr := os.ReadFile(path); readErr == nil {
				if existing := strings.TrimSpace(string(data)); installationIDPattern.MatchString(existing) {
					return existing, nil
				}
			}
		}
		return "", fmt.Errorf("installation identity: create %s: %w", path, err)
	}
	if _, err := file.WriteString(id + "\n"); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("installation identity: write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("installation identity: close %s: %w", path, err)
	}
	return id, nil
}

// containerOwnership classifies a container's installation label relative to ours.
type containerOwnership int

const (
	// ownershipLegacy means the container predates installation labels. Legacy
	// containers may be adopted when a live DB record matches, but are never
	// purged automatically because ownership cannot be proven.
	ownershipLegacy containerOwnership = iota
	// ownershipOwned means the container carries this installation's identity.
	ownershipOwned
	// ownershipForeign means the container belongs to another installation (or
	// this process has no identity configured), so it must never be touched.
	ownershipForeign
)

func classifyOwnership(containerInstallationID, ourInstallationID string) containerOwnership {
	theirs := strings.TrimSpace(containerInstallationID)
	ours := strings.TrimSpace(ourInstallationID)
	if theirs == "" {
		return ownershipLegacy
	}
	if ours == "" || theirs != ours {
		return ownershipForeign
	}
	return ownershipOwned
}

// applyInstallationLabel stamps the current installation identity onto a label map
// built for a container this process is about to create.
func (c *Client) applyInstallationLabel(labels map[string]string) {
	if c == nil || labels == nil {
		return
	}
	if id := strings.TrimSpace(c.config.InstallationID); id != "" {
		labels[LabelInstallation] = id
	}
}

// filterByInstallation narrows a container lookup to this installation when an
// identity is configured. Callers keep their deterministic-name fallback, so
// legacy containers without the label remain reachable.
func (c *Client) filterByInstallation(filters client.Filters) client.Filters {
	if c == nil {
		return filters
	}
	if id := strings.TrimSpace(c.config.InstallationID); id != "" {
		filters.Add("label", fmt.Sprintf("%s=%s", LabelInstallation, id))
	}
	return filters
}

// validateConflictOwnership guards the name-conflict adoption path. A 409 means a
// container already holds the deterministic name Carbon Panel wants; adopting or
// removing it blindly could steal or destroy another installation's server.
// Foreign installation identities and foreign server/module IDs are rejected;
// unlabeled (legacy) containers keep the previous adopt/remove behavior.
func (c *Client) validateConflictOwnership(containerID string, existingLabels, desiredLabels map[string]string) error {
	ours := strings.TrimSpace(c.config.InstallationID)
	if ours != "" {
		if theirs := strings.TrimSpace(existingLabels[LabelInstallation]); theirs != "" && theirs != ours {
			return fmt.Errorf("container %s belongs to another Carbon Panel installation (%s=%q); refusing to adopt or remove it", shortID(containerID), LabelInstallation, theirs)
		}
	}
	for _, key := range []string{"carbon-panel.server.id", "carbon-panel.module.id"} {
		want := strings.TrimSpace(desiredLabels[key])
		got := strings.TrimSpace(existingLabels[key])
		if want != "" && got != "" && got != want {
			return fmt.Errorf("container %s belongs to %s=%q, not %q; refusing to adopt or remove it", shortID(containerID), key, got, want)
		}
	}
	return nil
}
