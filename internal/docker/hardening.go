package docker

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/athNdev/carbon-panel/pkg/files"
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

// Confinement errors returned by the volume/device/network validators below.
// Unlike ErrElevatedDockerPermissionRequired these are NOT permission-gated:
// a host-path bind mount outside the caller's safe roots, a host device
// mapping, or host/none networking breaks container confinement for ANY
// caller, including holders of the elevated docker permission, so they are
// rejected outright (map to Connect CodeInvalidArgument at the RPC layer).
var (
	ErrDockerVolumeOutsideSafeRoot = errors.New("docker volume source is outside the allowed roots")
	ErrDockerDevicesNotAllowed     = errors.New("docker overrides must not request host device mappings")
	ErrDockerNetworkModeNotAllowed = errors.New("docker network mode is not allowed")
)

// ServerOverrideSafeRoots returns the safe roots for per-server
// DockerOverrides volume sources: the server's own data path, plus its
// host-translated form for when the panel itself runs in a container
// (TranslateToHostPath is a no-op on host runs, so including both views is
// always safe).
func ServerOverrideSafeRoots(serverDataPath string) []string {
	return expandWithHostTranslation([]string{serverDataPath})
}

// ModuleSafeRoots returns the safe roots for module volume mounts: the
// owning server's data path (builtin templates mount
// {{server.data_path}}/...) and the global backup directory (the mc-backup
// template mounts {{config.storage.backup_dir}}). Empty roots are dropped;
// with no safe roots every absolute bind source is rejected (fail closed).
func ModuleSafeRoots(serverDataPath, backupDir string) []string {
	return expandWithHostTranslation([]string{serverDataPath, backupDir})
}

func expandWithHostTranslation(roots []string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		for _, cand := range []string{r, TranslateToHostPath(r)} {
			if cand == "" {
				continue
			}
			if _, ok := seen[cand]; !ok {
				seen[cand] = struct{}{}
				out = append(out, cand)
			}
		}
	}
	return out
}

// ValidateVolumeSource enforces host-path confinement for a single mount
// source. Docker named volumes (explicit Type "volume", an empty source, or
// a non-absolute source name) reference daemon-managed storage rather than a
// host path and are always allowed. Anything else that names an absolute
// host path (a bind mount, the default when Type is empty) must resolve
// inside one of safeRoots, checked with files.Within so sibling-prefix
// (/data/srv-evil) and ".." escapes are rejected.
func ValidateVolumeSource(source, mountType string, safeRoots ...string) error {
	if strings.EqualFold(strings.TrimSpace(mountType), "volume") {
		return nil
	}
	src := strings.TrimSpace(source)
	if src == "" {
		return nil
	}
	if !filepath.IsAbs(src) {
		return nil
	}
	for _, root := range safeRoots {
		if root == "" {
			continue
		}
		if files.Within(root, src) {
			return nil
		}
	}
	return fmt.Errorf("%w: bind-mount source %q is outside the allowed roots", ErrDockerVolumeOutsideSafeRoot, source)
}

// ValidateDockerVolumeOverrides applies ValidateVolumeSource to every
// DockerOverrides volume entry (Path A: server overrides).
func ValidateDockerVolumeOverrides(volumes []*v1.VolumeMount, safeRoots ...string) error {
	for i, vol := range volumes {
		if vol == nil {
			continue
		}
		if err := ValidateVolumeSource(vol.GetSource(), vol.GetType(), safeRoots...); err != nil {
			return fmt.Errorf("volumes[%d]: %w", i, err)
		}
	}
	return nil
}

// ValidateModuleVolumeMounts applies ValidateVolumeSource to every module
// volume entry (Path B: module volumes, post alias-substitution).
func ValidateModuleVolumeMounts(volumes []ModuleVolumeMount, safeRoots ...string) error {
	for i, vol := range volumes {
		if err := ValidateVolumeSource(vol.Source, vol.Type, safeRoots...); err != nil {
			return fmt.Errorf("volumes[%d]: %w", i, err)
		}
	}
	return nil
}

// ValidateDockerDevices rejects any host device mapping. There is no
// legitimate panel flow that maps host devices into server containers.
func ValidateDockerDevices(devices []string) error {
	for _, d := range devices {
		if strings.TrimSpace(d) != "" {
			return fmt.Errorf("%w: %q", ErrDockerDevicesNotAllowed, d)
		}
	}
	return nil
}

// ValidateDockerNetworkMode rejects the confinement-breaking network modes.
// "host" shares the host network namespace and "none" breaks the panel's
// proxy/networking assumptions; anything else (empty/default, named
// networks) is left alone.
func ValidateDockerNetworkMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "host", "none":
		return fmt.Errorf("%w: %q", ErrDockerNetworkModeNotAllowed, mode)
	default:
		return nil
	}
}

// ValidateDockerOverridesConfinement is the single authoritative entry point
// for the volume/device/network gap: it rejects host-breakout-capable
// DockerOverrides for ALL callers (no permission bypass). Wire it in before
// overrides are persisted (CreateServer/UpdateServer) in addition to the
// permission-gated ValidateDockerOverrides above.
func ValidateDockerOverridesConfinement(overrides *v1.DockerOverrides, safeRoots ...string) error {
	if overrides == nil {
		return nil
	}
	if err := ValidateDockerDevices(overrides.GetDevices()); err != nil {
		return err
	}
	if err := ValidateDockerNetworkMode(overrides.GetNetworkMode()); err != nil {
		return err
	}
	if err := ValidateDockerVolumeOverrides(overrides.GetVolumes(), safeRoots...); err != nil {
		return err
	}
	return nil
}
