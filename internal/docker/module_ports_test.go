// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package docker

import (
	"testing"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestPublishModuleHostPort(t *testing.T) {
	if publishModuleHostPort(&v1.ModulePort{HostPort: 19132, Protocol: "udp", ProxyEnabled: true}) {
		t.Fatal("proxied module port without expose_public must stay unpublished")
	}
	if !publishModuleHostPort(&v1.ModulePort{HostPort: 19132, Protocol: "udp", ProxyEnabled: true, ExposePublic: true}) {
		t.Fatal("Geyser-style public UDP must be published")
	}
	if !publishModuleHostPort(&v1.ModulePort{HostPort: 8080, Protocol: "tcp"}) {
		t.Fatal("unproxied module port must be published")
	}
	if publishModuleHostPort(&v1.ModulePort{HostPort: 0, ExposePublic: true}) {
		t.Fatal("unallocated host port must not be published")
	}
}
