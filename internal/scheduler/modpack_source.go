package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/athNdev/carbon-panel/pkg/files"
)

// Validation for user-supplied modpack source coordinates.
//
// A scheduled modpack-update task lets a user supply a git URL, a branch and a
// subfolder. Those values reach `exec.CommandContext("git", ...)`, which makes
// two distinct classes of bug reachable:
//
//  1. Argument injection. git parses any argument beginning with "-" as an
//     OPTION, not a value. `git fetch origin <branch>` therefore executes an
//     arbitrary program when branch is
//     `--upload-pack=touch /tmp/pwned`. Verified against real git: the injected
//     command runs. Passing `--` before the positional arguments closes this,
//     and the branch allowlist below rejects it a second time.
//  2. Transport abuse. git's `ext::` transport runs a shell command, and
//     `file://` reaches the local filesystem. Only https:// is accepted.
//
// The subfolder is a path component, so `..` in it escapes the clone cache and
// lets a task rsync arbitrary host files into (or out of) a server directory.

var (
	// ErrInvalidModpackSource is returned for any rejected source coordinate.
	ErrInvalidModpackSource = errors.New("invalid modpack source")

	// gitRefPattern is a conservative allowlist for a branch name: it must
	// start with an alphanumeric (so it can never begin with "-") and may
	// then contain alphanumerics, dot, underscore, slash and hyphen.
	gitRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

	// modpackSourceAllowedSchemes is the set of URL schemes a modpack may be
	// fetched from. https only: the field is documented as an
	// https://github.com/... URL in the proto and the UI renders an HTTPS
	// placeholder, so http:// buys no legitimate use while allowing
	// man-in-the-middle substitution of the modpack contents.
	modpackSourceAllowedSchemes = map[string]bool{
		"https": true,
	}
)

// validateGitBranch rejects any branch that git could reinterpret as an
// option or that contains a traversal sequence.
func validateGitBranch(branch string) error {
	if branch == "" {
		return fmt.Errorf("%w: branch is empty", ErrInvalidModpackSource)
	}
	// Explicit, belt-and-braces: gitRefPattern already forbids a leading "-"
	// but the reason matters enough to state.
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("%w: branch %q must not start with '-'", ErrInvalidModpackSource, branch)
	}
	if strings.Contains(branch, "..") {
		return fmt.Errorf("%w: branch %q must not contain '..'", ErrInvalidModpackSource, branch)
	}
	if strings.ContainsAny(branch, "\x00\n\r\t ") {
		return fmt.Errorf("%w: branch %q contains illegal whitespace", ErrInvalidModpackSource, branch)
	}
	if !gitRefPattern.MatchString(branch) {
		return fmt.Errorf("%w: branch %q contains unsupported characters", ErrInvalidModpackSource, branch)
	}
	// git refuses a ref ending in ".lock"; reject it up front for a clearer
	// error than the one git would emit mid-task.
	if strings.HasSuffix(branch, ".lock") {
		return fmt.Errorf("%w: branch %q must not end with '.lock'", ErrInvalidModpackSource, branch)
	}
	return nil
}

// validateGitURL rejects any modpack URL that is not a plain https URL, which
// excludes the `ext::` (shell execution), `file://` (local filesystem) and
// `-`-prefixed (argument injection) forms.
func validateGitURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("%w: git url is empty", ErrInvalidModpackSource)
	}
	if strings.ContainsAny(raw, "\x00\n\r") {
		return fmt.Errorf("%w: git url contains illegal characters", ErrInvalidModpackSource)
	}
	if strings.HasPrefix(raw, "-") {
		return fmt.Errorf("%w: git url must not start with '-'", ErrInvalidModpackSource)
	}
	// A URL with no scheme is parsed by git as a local path. Require a scheme.
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: git url is not a valid URL: %v", ErrInvalidModpackSource, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if !modpackSourceAllowedSchemes[scheme] {
		return fmt.Errorf("%w: git url scheme %q is not allowed (allowed: https)", ErrInvalidModpackSource, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: git url has no host", ErrInvalidModpackSource)
	}
	return nil
}

// resolveModpackSourceDir resolves the subfolder against the clone cache and
// guarantees the result stays inside it.
//
// It deliberately does NOT use filepath.Join alone: Join cleans "..", which is
// exactly what turns a malicious subfolder into an escape. The cleaned path is
// then re-checked against the cache root with files.Within, which is
// filepath.Rel based and so rejects both ".." escapes and prefix confusion
// (a sibling directory named "<cache>-evil" is not inside "<cache>").
func resolveModpackSourceDir(cacheDir, subfolder string) (string, error) {
	if cacheDir == "" {
		return "", fmt.Errorf("%w: empty cache directory", ErrInvalidModpackSource)
	}
	if filepath.IsAbs(subfolder) {
		return "", fmt.Errorf("%w: target subfolder %q must be relative", ErrInvalidModpackSource, subfolder)
	}
	// Reject a traversal segment outright rather than relying on confinement
	// alone, so the error message names the actual cause.
	for _, seg := range strings.FieldsFunc(subfolder, func(r rune) bool { return r == '/' || r == os.PathSeparator }) {
		if seg == ".." {
			return "", fmt.Errorf("%w: target subfolder %q must not contain '..'", ErrInvalidModpackSource, subfolder)
		}
	}

	srcDir := cacheDir
	if subfolder != "" && subfolder != "." {
		srcDir = filepath.Join(cacheDir, subfolder)
	}

	if !files.Within(cacheDir, srcDir) {
		return "", fmt.Errorf("%w: target subfolder %q escapes the modpack cache", ErrInvalidModpackSource, subfolder)
	}
	return srcDir, nil
}

// cloneFn is the single point at which the scheduler shells out to
// `git clone` for a user-supplied URL, and fetchFn the equivalent for
// `git fetch`. They are package-level variables so a test can substitute a
// local repository for a remote HTTPS one. Keeping them as narrow function
// seams - rather than opening up the URL allowlist - means the integration
// test can still exercise clone/stage/sync end to end without production
// ever accepting a local path (which would be a host-read primitive).
//
// Every other git invocation in the modpack flow (rev-parse, diff, reset) takes
// no user-controlled argument, so it needs no seam.
var (
	cloneFn = defaultGitClone
	fetchFn = defaultGitFetch
)

func defaultGitClone(ctx context.Context, url, branch, dir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "-b", branch, "--", url, dir)
	cmd.Env = hardenedGitEnv(os.Environ())
	return cmd.CombinedOutput()
}

func defaultGitFetch(ctx context.Context, dir, branch string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "fetch", "origin", "--", branch)
	cmd.Env = hardenedGitEnv(os.Environ())
	return cmd.CombinedOutput()
}

// ValidateModpackUpdateConfigJSON parses a modpack-update task config JSON and
// validates its git source coordinates. Exported so the task RPC service can
// reject a hostile config at create/update time with a clear InvalidArgument,
// instead of letting it sit in the database and fail (or, before this fix,
// execute) hours later when the schedule first fires.
func ValidateModpackUpdateConfigJSON(raw string) error {
	var cfg ModpackUpdateTaskConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return fmt.Errorf("invalid modpack update config: %w", err)
	}
	// An empty git_url is reported by the executor with a clearer message
	// ("git_url is required for modpack update task"); do not shadow it.
	if cfg.GitURL == "" {
		return nil
	}
	return validateModpackSource(cfg)
}

// hardenedGitEnv returns a copy of env for a git child process with the
// transports that can execute code or reach the local filesystem disabled.
// This is defence in depth: the URL allowlist already rejects `ext::` and
// `file://`, but the task config is user data and a second barrier is cheap.
func hardenedGitEnv(env []string) []string {
	out := make([]string, 0, len(env)+2)
	// Strip any inherited value first so we cannot append a duplicate that
	// git reads ambiguously.
	filtered := out[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_ALLOW_PROTOCOL=") || strings.HasPrefix(kv, "GIT_PROTOCOL_FROM_USER=") {
			continue
		}
		filtered = append(filtered, kv)
	}
	out = append(filtered,
		"GIT_ALLOW_PROTOCOL=https",
		"GIT_PROTOCOL_FROM_USER=0",
	)
	return out
}

// validateModpackSource validates every user-supplied source coordinate in one
// call. It is called both when a task is created/updated (so the user gets an
// InvalidArgument error instead of a failed run hours later) and again
// immediately before the git commands execute (so a task row written before
// this fix, or by any other writer, is still safe).
func validateModpackSource(cfg ModpackUpdateTaskConfig) error {
	if err := validateGitURL(cfg.GitURL); err != nil {
		return err
	}
	if err := validateGitBranch(cfg.Branch); err != nil {
		return err
	}
	return nil
}
