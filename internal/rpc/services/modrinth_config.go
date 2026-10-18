// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package services

import storage "github.com/athNdev/carbon-panel/internal/db"

// configureModrinthModpack keeps project selection and version pinning in the
// separate environment variables expected by itzg/docker-minecraft-server.
func configureModrinthModpack(config *storage.ServerConfig, projectID, version string) {
	config.ModrinthModpack = &projectID
	dependencies := "required"
	config.ModrinthDownloadDependencies = &dependencies

	if version != "" && version != "latest" {
		config.ModrinthVersion = &version
		config.ModrinthModpackVersionType = nil
		return
	}

	config.ModrinthVersion = nil
	versionType := "release"
	config.ModrinthModpackVersionType = &versionType
}
