package docker

import (
	"slices"
	"testing"

	"github.com/docker/docker/api/types/container"
	v1 "github.com/athNdev/mineserver/pkg/proto/mineserver/v1"
)

func TestContainerHardening_ApplyOverrides(t *testing.T) {
	// Base hardened hostConfig
	pidsLimit := int64(512)
	hostConfig := &container.HostConfig{
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges:true"},
		Resources: container.Resources{
			PidsLimit: &pidsLimit,
		},
	}
	config := &container.Config{}

	if !slices.Contains(hostConfig.CapDrop, "ALL") {
		t.Errorf("expected CapDrop ALL by default")
	}
	if !slices.Contains(hostConfig.SecurityOpt, "no-new-privileges:true") {
		t.Errorf("expected no-new-privileges:true by default")
	}
	if *hostConfig.Resources.PidsLimit != 512 {
		t.Errorf("expected PidsLimit 512 by default")
	}

	// Test custom overrides
	customPids := int64(1024)
	overrides := &v1.DockerOverrides{
		CapAdd:      []string{"SYS_NICE"},
		PidsLimit:   customPids,
		CpusetCpus:  "0,1,2",
		ReadOnly:    true,
		SecurityOpt: []string{"no-new-privileges:true", "apparmor:unconfined"},
	}

	ApplyOverrides(overrides, config, hostConfig)

	if hostConfig.Resources.CpusetCpus != "0,1,2" {
		t.Errorf("expected CpusetCpus 0,1,2, got %s", hostConfig.Resources.CpusetCpus)
	}
	if !slices.Contains(hostConfig.CapAdd, "SYS_NICE") {
		t.Errorf("expected SYS_NICE in CapAdd")
	}
	if *hostConfig.Resources.PidsLimit != 1024 {
		t.Errorf("expected PidsLimit overridden to 1024")
	}
	if !hostConfig.ReadonlyRootfs {
		t.Errorf("expected ReadonlyRootfs to be true")
	}
	if !slices.Contains(hostConfig.SecurityOpt, "apparmor:unconfined") {
		t.Errorf("expected apparmor:unconfined in SecurityOpt")
	}
}
