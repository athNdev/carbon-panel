// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package docker

import (
	"net/netip"
	"testing"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/moby/moby/api/types/network"
)

func TestAdditionalPortBindings(t *testing.T) {
	ports := []*v1.AdditionalPort{
		{Name: "map", ContainerPort: 8100, HostPort: 8100, Protocol: "tcp"},
		{Name: "voice", ContainerPort: 24454, HostPort: 24454, Protocol: "udp", ExposePublic: true},
	}

	proxied := additionalPortBindings(ports, true)
	if got := hostIP(t, proxied, "8100/tcp"); got != "127.0.0.1" {
		t.Fatalf("proxied private port bound %s", got)
	}
	if got := hostIP(t, proxied, "24454/udp"); got != "0.0.0.0" {
		t.Fatalf("proxied public port bound %s", got)
	}

	direct := additionalPortBindings(ports, false)
	if got := hostIP(t, direct, "8100/tcp"); got != "0.0.0.0" {
		t.Fatalf("unproxied port bound %s", got)
	}
}

func hostIP(t *testing.T, bindings network.PortMap, key string) string {
	t.Helper()
	port, err := network.ParsePort(key)
	if err != nil {
		t.Fatal(err)
	}
	got := bindings[port]
	if len(got) != 1 {
		t.Fatalf("%s bindings = %#v", key, got)
	}
	if got[0].HostIP != netip.MustParseAddr(got[0].HostIP.String()) {
		t.Fatalf("host ip %v", got[0].HostIP)
	}
	return got[0].HostIP.String()
}
