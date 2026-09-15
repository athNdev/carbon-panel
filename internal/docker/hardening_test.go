package docker

import (
	"errors"
	"slices"
	"testing"

	"github.com/docker/docker/api/types/container"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
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

// --- Bug 2 regression tests: privileged Docker overrides require the
// elevated servers/manage_docker_privileged permission ---
//
// ApplyOverrides itself intentionally still applies whatever is stored on
// the server record unconditionally (see TestContainerHardening_ApplyOverrides
// above) - overrides only ever reach it after already having been vetted by
// ValidateDockerOverrides at the point they were submitted via
// CreateServer/UpdateServer (internal/rpc/services/server.go). These tests
// cover that gating function directly.

func TestIsPrivilegedDockerOverride_BenignFieldsAreNotPrivileged(t *testing.T) {
	benign := &v1.DockerOverrides{
		PidsLimit:   1024,
		CpusetCpus:  "0,1,2",
		ReadOnly:    true,
		CapDrop:     []string{"ALL", "NET_RAW"},
		SecurityOpt: []string{"no-new-privileges:true"},
		MemoryLimit: 2048,
		ShmSize:     67108864,
	}
	if IsPrivilegedDockerOverride(benign) {
		t.Errorf("expected benign overrides to not be flagged as privileged")
	}
	if IsPrivilegedDockerOverride(nil) {
		t.Errorf("expected nil overrides to not be flagged as privileged")
	}
}

func TestIsPrivilegedDockerOverride_DangerousFields(t *testing.T) {
	tests := []struct {
		name      string
		overrides *v1.DockerOverrides
	}{
		{"privileged mode", &v1.DockerOverrides{Privileged: true}},
		{"any CapAdd", &v1.DockerOverrides{CapAdd: []string{"SYS_NICE"}}},
		{"CapAdd SYS_ADMIN", &v1.DockerOverrides{CapAdd: []string{"SYS_ADMIN"}}},
		{"apparmor:unconfined", &v1.DockerOverrides{SecurityOpt: []string{"apparmor:unconfined"}}},
		{"apparmor=unconfined", &v1.DockerOverrides{SecurityOpt: []string{"apparmor=unconfined"}}},
		{"seccomp:unconfined", &v1.DockerOverrides{SecurityOpt: []string{"seccomp:unconfined"}}},
		{"seccomp=unconfined", &v1.DockerOverrides{SecurityOpt: []string{"seccomp=unconfined"}}},
		{"label:disable", &v1.DockerOverrides{SecurityOpt: []string{"label:disable"}}},
		{"label=disable", &v1.DockerOverrides{SecurityOpt: []string{"label=disable"}}},
		{"mixed with benign", &v1.DockerOverrides{
			SecurityOpt: []string{"no-new-privileges:true", "apparmor:unconfined"},
			CapDrop:     []string{"ALL"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !IsPrivilegedDockerOverride(tt.overrides) {
				t.Errorf("expected %q to be flagged as a privileged override", tt.name)
			}
		})
	}
}

func TestIsPrivilegedDockerOverride_BenignSecurityOptsAreNotFlagged(t *testing.T) {
	// A custom/named seccomp profile tightens confinement rather than
	// disabling it, and is unrelated to the AppArmor/seccomp/label keys we
	// specifically gate.
	overrides := &v1.DockerOverrides{
		SecurityOpt: []string{"no-new-privileges:true", "seccomp=/path/to/custom-profile.json"},
	}
	if IsPrivilegedDockerOverride(overrides) {
		t.Errorf("expected a custom seccomp profile path to not be flagged as privileged")
	}
}

func TestValidateDockerOverrides_DangerousFieldsRejectedWithoutPermission(t *testing.T) {
	overrides := &v1.DockerOverrides{
		Privileged:  true,
		CapAdd:      []string{"SYS_ADMIN"},
		SecurityOpt: []string{"apparmor:unconfined"},
	}

	// Caller without the elevated permission -> rejected.
	if err := ValidateDockerOverrides(overrides, false); err == nil {
		t.Fatal("expected privileged overrides to be rejected for a caller without elevated permission")
	} else if !errors.Is(err, ErrElevatedDockerPermissionRequired) {
		t.Errorf("expected ErrElevatedDockerPermissionRequired, got %v", err)
	}
}

func TestValidateDockerOverrides_DangerousFieldsAllowedWithPermission(t *testing.T) {
	overrides := &v1.DockerOverrides{
		Privileged:  true,
		CapAdd:      []string{"SYS_ADMIN"},
		SecurityOpt: []string{"apparmor:unconfined"},
	}

	// Caller WITH the elevated permission -> succeeds.
	if err := ValidateDockerOverrides(overrides, true); err != nil {
		t.Errorf("expected privileged overrides to be allowed for a caller with elevated permission, got %v", err)
	}
}

func TestValidateDockerOverrides_BenignOverridesAlwaysAllowed(t *testing.T) {
	benign := &v1.DockerOverrides{
		PidsLimit:   1024,
		CpusetCpus:  "0,1,2",
		ReadOnly:    true,
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges:true"},
	}

	// Benign overrides are unaffected regardless of elevated permission.
	if err := ValidateDockerOverrides(benign, false); err != nil {
		t.Errorf("expected benign overrides to be allowed without elevated permission, got %v", err)
	}
	if err := ValidateDockerOverrides(benign, true); err != nil {
		t.Errorf("expected benign overrides to be allowed with elevated permission, got %v", err)
	}
	if err := ValidateDockerOverrides(nil, false); err != nil {
		t.Errorf("expected nil overrides to be allowed, got %v", err)
	}
}
