package docker

import (
	"errors"
	"fmt"
	"strings"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// ErrElevatedDockerPermissionRequired is returned by ValidateDockerOverrides
// when the caller requested a host-breakout-capable Docker override
// (Privileged mode, CapAdd, or a confinement-weakening SecurityOpt) without
// the elevated permission.
var ErrElevatedDockerPermissionRequired = errors.New("docker overrides request privileged mode, added capabilities, or a confinement-weakening security option; this requires the servers/manage_docker_privileged permission")

// dangerousSecurityOptKeys maps the "key" portion of a --security-opt value
// (split on "=" or the legacy ":") to the value(s) that weaken or disable
// container confinement. Any other key (e.g. "no-new-privileges", or a
// named/custom seccomp profile) is left alone as benign.
var dangerousSecurityOptValues = map[string]string{
	"apparmor": "unconfined",
	"seccomp":  "unconfined",
	"label":    "disable",
}

// isDangerousSecurityOpt reports whether a single --security-opt entry
// weakens or disables container confinement (AppArmor, seccomp, or SELinux
// labeling). Docker accepts both "key=value" and the legacy "key:value"
// separator, so both are handled here.
func isDangerousSecurityOpt(opt string) bool {
	normalized := strings.ToLower(strings.TrimSpace(opt))
	key, value, found := strings.Cut(normalized, "=")
	if !found {
		key, value, found = strings.Cut(normalized, ":")
	}
	if !found {
		return false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	dangerousValue, ok := dangerousSecurityOptValues[key]
	return ok && value == dangerousValue
}

// IsPrivilegedDockerOverride reports whether the given overrides request any
// genuinely dangerous, host-breakout-capable setting:
//   - full --privileged mode
//   - any added Linux capability (CapAdd)
//   - a confinement-weakening SecurityOpt (apparmor:unconfined,
//     seccomp:unconfined, label:disable, and their "=" spellings)
//
// Benign fields (PidsLimit, CpusetCpus, ReadOnly, CapDrop,
// "no-new-privileges:true", etc.) never make this return true.
func IsPrivilegedDockerOverride(overrides *v1.DockerOverrides) bool {
	if overrides == nil {
		return false
	}
	if overrides.GetPrivileged() {
		return true
	}
	if len(overrides.GetCapAdd()) > 0 {
		return true
	}
	for _, opt := range overrides.GetSecurityOpt() {
		if isDangerousSecurityOpt(opt) {
			return true
		}
	}
	return false
}

// ValidateDockerOverrides enforces that only callers holding the elevated
// Docker permission may request privileged-mode settings (see
// IsPrivilegedDockerOverride). It must be called with the caller's
// permission status BEFORE the overrides are persisted to a server record;
// once persisted, later container (re)creation trusts the stored value and
// does not re-check permission, since the caller at container-creation time
// (e.g. an operator starting an already-configured server) is not
// necessarily the same caller - or as privileged - as whoever originally
// set the overrides.
func ValidateDockerOverrides(overrides *v1.DockerOverrides, allowPrivileged bool) error {
	if allowPrivileged {
		return nil
	}
	if !IsPrivilegedDockerOverride(overrides) {
		return nil
	}
	return fmt.Errorf("%w", ErrElevatedDockerPermissionRequired)
}
