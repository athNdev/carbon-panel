package docker

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/docker/docker/api/types/container"
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

// --- Confinement regression tests: volumes/devices/network mode are rejected
// for ALL callers (no permission bypass), while legitimate mounts still pass.

func TestValidateVolumeSource_RejectsAbsoluteOutsideRoots(t *testing.T) {
	root := t.TempDir()
	evil := []string{
		"/",
		"/etc",
		"/etc/passwd",
		filepath.Join(root, "..", "..", "etc"), // ".." escape
		root + "-evil",                         // sibling prefix confusion
		filepath.Join(root+"-evil", "data"),
	}
	for _, src := range evil {
		if err := ValidateVolumeSource(src, "bind", root); err == nil {
			t.Errorf("expected bind source %q to be rejected outside root %q", src, root)
		} else if !errors.Is(err, ErrDockerVolumeOutsideSafeRoot) {
			t.Errorf("expected ErrDockerVolumeOutsideSafeRoot for %q, got %v", src, err)
		}
	}
}

func TestValidateVolumeSource_AllowsInsideRootAndNamedVolumes(t *testing.T) {
	root := t.TempDir()
	allowed := []struct {
		name      string
		source    string
		mountType string
		safeRoots []string
	}{
		{"root itself", root, "bind", []string{root}},
		{"subdir of root", filepath.Join(root, "mods"), "bind", []string{root}},
		{"empty type defaults to bind inside root", filepath.Join(root, "data"), "", []string{root}},
		{"named volume bare name", "mydata", "", []string{root}},
		{"named volume explicit type", "mydata", "volume", []string{root}},
		{"explicit volume type with absolute source", "/etc", "volume", []string{root}},
		{"explicit volume type mixed case", "/etc", "Volume", []string{root}},
		{"empty source", "", "bind", []string{root}},
		{"no safe roots but named volume", "mydata", "", nil},
	}
	for _, tt := range allowed {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateVolumeSource(tt.source, tt.mountType, tt.safeRoots...); err != nil {
				t.Errorf("expected source %q (type %q) to be allowed, got %v", tt.source, tt.mountType, err)
			}
		})
	}
}

func TestValidateVolumeSource_FailClosedWithNoRoots(t *testing.T) {
	if err := ValidateVolumeSource("/data/x", "bind"); err == nil {
		t.Error("expected absolute bind source to be rejected when no safe roots are configured")
	}
}

func TestValidateDockerDevices_RejectsNonEmpty(t *testing.T) {
	if err := ValidateDockerDevices([]string{"/dev/kvm:/dev/kvm"}); err == nil {
		t.Error("expected host device mapping to be rejected")
	} else if !errors.Is(err, ErrDockerDevicesNotAllowed) {
		t.Errorf("expected ErrDockerDevicesNotAllowed, got %v", err)
	}
	if err := ValidateDockerDevices(nil); err != nil {
		t.Errorf("expected nil devices to be allowed, got %v", err)
	}
	if err := ValidateDockerDevices([]string{"", "   "}); err != nil {
		t.Errorf("expected blank device entries to be allowed, got %v", err)
	}
}

func TestValidateDockerNetworkMode_RejectsHostAndNone(t *testing.T) {
	for _, mode := range []string{"host", "HOST", " host ", "none", "None", " NONE "} {
		if err := ValidateDockerNetworkMode(mode); err == nil {
			t.Errorf("expected network mode %q to be rejected", mode)
		} else if !errors.Is(err, ErrDockerNetworkModeNotAllowed) {
			t.Errorf("expected ErrDockerNetworkModeNotAllowed for %q, got %v", mode, err)
		}
	}
	for _, mode := range []string{"", "bridge", "carbon-net"} {
		if err := ValidateDockerNetworkMode(mode); err != nil {
			t.Errorf("expected network mode %q to be allowed, got %v", mode, err)
		}
	}
}

func TestValidateDockerOverridesConfinement_EndToEnd(t *testing.T) {
	root := t.TempDir()
	if err := ValidateDockerOverridesConfinement(nil, root); err != nil {
		t.Errorf("expected nil overrides to pass confinement, got %v", err)
	}
	benign := &v1.DockerOverrides{
		PidsLimit:  1024,
		CpusetCpus: "0,1",
		ReadOnly:   true,
		Volumes: []*v1.VolumeMount{
			{Source: "mydata", Target: "/data/extra", Type: "volume"},
		},
	}
	if err := ValidateDockerOverridesConfinement(benign, ServerOverrideSafeRoots(root)...); err != nil {
		t.Errorf("expected benign overrides with a named volume to pass confinement, got %v", err)
	}
	legitBind := &v1.DockerOverrides{
		Volumes: []*v1.VolumeMount{
			{Source: filepath.Join(root, "mods"), Target: "/data/mods"},
		},
	}
	if err := ValidateDockerOverridesConfinement(legitBind, ServerOverrideSafeRoots(root)...); err != nil {
		t.Errorf("expected bind mount inside the server data root to pass confinement, got %v", err)
	}
	hostile := &v1.DockerOverrides{
		Volumes:     []*v1.VolumeMount{{Source: "/", Target: "/host"}},
		Devices:     []string{"/dev/kvm:/dev/kvm"},
		NetworkMode: "host",
	}
	if err := ValidateDockerOverridesConfinement(hostile, ServerOverrideSafeRoots(root)...); err == nil {
		t.Error("expected hostile overrides to fail confinement")
	}
	// Confinement has no permission bypass: even the "everything" roots of an
	// elevated caller do not apply here — "/" is outside the server data root.
	onlyRoot := &v1.DockerOverrides{
		Volumes: []*v1.VolumeMount{{Source: "/etc", Target: "/host-etc"}},
	}
	if err := ValidateDockerOverridesConfinement(onlyRoot, ServerOverrideSafeRoots(root)...); err == nil {
		t.Error("expected /etc bind mount to fail confinement")
	}
}

func TestServerOverrideSafeRoots_ExpandsHostTranslation(t *testing.T) {
	roots := ServerOverrideSafeRoots("/data/srv")
	if len(roots) == 0 || roots[0] != "/data/srv" {
		t.Errorf("expected server data path to be the first safe root, got %v", roots)
	}
	if got := ServerOverrideSafeRoots(""); len(got) != 0 {
		t.Errorf("expected no safe roots for an empty data path, got %v", got)
	}
}
