// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import "testing"

func TestDockerTLSSkipVerifyRequiresEscape(t *testing.T) {
	t.Setenv("CARBON_ALLOW_INSECURE_DOCKER_TLS", "")
	if dockerTLSInsecure(true) {
		t.Fatal("node tls_skip_verify alone must not disable verification")
	}
	if dockerTLSInsecure(false) {
		t.Fatal("verification stays on when the node flag is false")
	}

	t.Setenv("CARBON_ALLOW_INSECURE_DOCKER_TLS", "1")
	if !dockerTLSInsecure(true) {
		t.Fatal("process escape plus the node flag may skip verification")
	}
	if dockerTLSInsecure(false) {
		t.Fatal("process escape without the node flag must keep verification")
	}
}
