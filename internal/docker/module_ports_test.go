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
