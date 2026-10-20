// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package module

import "testing"

func TestGeyserBedrockStaysPublic(t *testing.T) {
	port := geyserBedrockPort()
	if !port.ExposePublic {
		t.Fatal("Geyser Bedrock UDP must be marked public")
	}
	if port.Protocol != "udp" || port.ContainerPort != 19132 {
		t.Fatalf("port = %+v", port)
	}
}
