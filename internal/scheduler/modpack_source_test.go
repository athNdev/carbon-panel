package scheduler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/google/uuid"
)

// TestValidateGitBranchRejectsOptionInjection covers the argument-injection
// class. git parses any argument starting with "-" as an OPTION, so a branch
// of `--upload-pack=<cmd>` makes git run <cmd>. Verified against real git:
// `git fetch origin '--upload-pack=touch /tmp/pwned'` executes the command.
func TestValidateGitBranchRejectsOptionInjection(t *testing.T) {
	rejected := []struct {
		name   string
		branch string
	}{
		{"upload-pack RCE", "--upload-pack=touch /tmp/pwned"},
		{"leading dash", "-b"},
		{"single dash", "-"},
		{"double dash", "--"},
		{"dash prefix on normal name", "-main"},
		{"traversal", "main/../../etc"},
		{"bare traversal", ".."},
		{"embedded newline", "main\n--upload-pack=id"},
		{"embedded space", "main --upload-pack=id"},
		{"embedded tab", "main\tid"},
		{"nul byte", "main\x00"},
		{"lock suffix", "main.lock"},
		{"empty", ""},
		{"shell metachars", "main;id"},
		{"shell substitution", "main$(id)"},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateGitBranch(tc.branch); err == nil {
				t.Fatalf("expected rejection of branch %q, got nil error", tc.branch)
			}
		})
	}

	accepted := []string{"main", "feature/x", "release-1.2", "v1.0.0", "fix_a/b-c"}
	for _, branch := range accepted {
		t.Run("accepts "+branch, func(t *testing.T) {
			if err := validateGitBranch(branch); err != nil {
				t.Fatalf("expected %q to be accepted, got %v", branch, err)
			}
		})
	}
}

// TestValidateGitURLRejectsDangerousTransports covers git's transports that
// execute a shell or reach the local filesystem.
func TestValidateGitURLRejectsDangerousTransports(t *testing.T) {
	rejected := []struct {
		name string
		raw  string
	}{
		{"ext transport shell", "ext::sh -c id>/tmp/pwned"},
		{"ext transport with space", "ext:: sh -c id"},
		{"file scheme", "file:///etc/passwd"},
		{"git scheme", "git://evil.example/repo.git"},
		{"http scheme", "http://evil.example/repo.git"},
		{"ftp scheme", "ftp://evil.example/repo.git"},
		{"local path", "/srv/mods/mymod"},
		{"relative path", "../repo"},
		{"leading dash", "--upload-pack=id"},
		{"scheme with no host", "https://"},
		{"empty", ""},
		{"embedded newline", "https://ok.example/r.git\nid"},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateGitURL(tc.raw); err == nil {
				t.Fatalf("expected rejection of url %q, got nil error", tc.raw)
			}
		})
	}

	accepted := []string{
		"https://github.com/user/modpack.git",
		"https://gitlab.com/group/sub/repo.git",
		"https://codeberg.org/user/repo",
	}
	for _, raw := range accepted {
		t.Run("accepts "+raw, func(t *testing.T) {
			if err := validateGitURL(raw); err != nil {
				t.Fatalf("expected %q to be accepted, got %v", raw, err)
			}
		})
	}
}

// TestResolveModpackSourceDirConfinement covers the subfolder traversal that
// would otherwise let a task rsync arbitrary host files into a server dir.
// filepath.Join alone is NOT sufficient: it cleans "..", turning the escape
// into a real path.
func TestResolveModpackSourceDirConfinement(t *testing.T) {
	cache := t.TempDir()
	nested := filepath.Join(cache, "mods", "pack")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rejected := []string{
		"../..",
		"../../etc",
		"../outside",
		"mods/../../escape",
		"a/b/../../../..",
		"/etc",
		"/",
		"./../..",
	}
	for _, sub := range rejected {
		t.Run("rejects "+sub, func(t *testing.T) {
			got, err := resolveModpackSourceDir(cache, sub)
			if err == nil {
				t.Fatalf("expected rejection of subfolder %q, got %q", sub, got)
			}
			if got != "" {
				t.Fatalf("expected empty path on rejection, got %q", got)
			}
		})
	}

	t.Run("accepts empty subfolder", func(t *testing.T) {
		got, err := resolveModpackSourceDir(cache, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != cache {
			t.Fatalf("got %q, want %q", got, cache)
		}
	})

	t.Run("accepts dot", func(t *testing.T) {
		got, err := resolveModpackSourceDir(cache, ".")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != cache {
			t.Fatalf("got %q, want %q", got, cache)
		}
	})

	t.Run("accepts nested subfolder", func(t *testing.T) {
		got, err := resolveModpackSourceDir(cache, "mods/pack")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nested {
			t.Fatalf("got %q, want %q", got, nested)
		}
	})

	t.Run("rejects prefix-confusion sibling", func(t *testing.T) {
		// "<cache>-evil" is a sibling, not a child. A naive strings.HasPrefix
		// containment check would wrongly accept it.
		sibling := cache + "-evil"
		if err := os.MkdirAll(filepath.Join(sibling, "mods"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if _, err := resolveModpackSourceDir(cache, sibling+"/mods"); err == nil {
			t.Fatal("expected prefix-confusion sibling to be rejected")
		}
	})
}

// TestValidateModpackUpdateConfigJSON checks the RPC-facing entry point.
func TestValidateModpackUpdateConfigJSON(t *testing.T) {
	t.Run("rejects hostile branch", func(t *testing.T) {
		cfg := `{"git_url":"https://github.com/u/r.git","branch":"--upload-pack=id"}`
		if err := ValidateModpackUpdateConfigJSON(cfg); err == nil {
			t.Fatal("expected rejection of hostile branch config")
		}
	})

	t.Run("rejects ext transport", func(t *testing.T) {
		cfg := `{"git_url":"ext::sh -c id>/tmp/pwned","branch":"main"}`
		if err := ValidateModpackUpdateConfigJSON(cfg); err == nil {
			t.Fatal("expected rejection of ext:: url config")
		}
	})

	t.Run("accepts valid config", func(t *testing.T) {
		cfg := `{"git_url":"https://github.com/u/r.git","branch":"main"}`
		if err := ValidateModpackUpdateConfigJSON(cfg); err != nil {
			t.Fatalf("expected valid config accepted, got %v", err)
		}
	})

	t.Run("empty git url defers to executor", func(t *testing.T) {
		if err := ValidateModpackUpdateConfigJSON(`{"branch":"main"}`); err != nil {
			t.Fatalf("empty git_url should defer to the executor, got %v", err)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		if err := ValidateModpackUpdateConfigJSON(`{not json`); err == nil {
			t.Fatal("expected malformed json to be rejected")
		}
	})
}

// TestHardenedGitEnvDisablesDangerousTransports verifies the child-process
// environment hardening, including that an inherited value cannot survive and
// create an ambiguous duplicate.
func TestHardenedGitEnvDisablesDangerousTransports(t *testing.T) {
	env := hardenedGitEnv([]string{
		"PATH=/usr/bin",
		"GIT_ALLOW_PROTOCOL=ext",
		"GIT_PROTOCOL_FROM_USER=1",
	})
	joined := strings.Join(env, "\n")

	if !strings.Contains(joined, "GIT_ALLOW_PROTOCOL=https") {
		t.Error("expected GIT_ALLOW_PROTOCOL=https")
	}
	if !strings.Contains(joined, "GIT_PROTOCOL_FROM_USER=0") {
		t.Error("expected GIT_PROTOCOL_FROM_USER=0")
	}
	if strings.Contains(joined, "GIT_ALLOW_PROTOCOL=ext") {
		t.Error("inherited dangerous protocol allow-list survived")
	}
	if strings.Count(joined, "GIT_ALLOW_PROTOCOL=") != 1 {
		t.Error("duplicate GIT_ALLOW_PROTOCOL entries would be ambiguous to git")
	}
	if !strings.Contains(joined, "PATH=/usr/bin") {
		t.Error("unrelated env entries must be preserved")
	}
}

// TestValidateModpackSource covers the combined check used at execution time.
func TestValidateModpackSource(t *testing.T) {
	if err := validateModpackSource(ModpackUpdateTaskConfig{
		GitURL: "https://github.com/u/r.git", Branch: "main",
	}); err != nil {
		t.Fatalf("valid source rejected: %v", err)
	}
	for _, cfg := range []ModpackUpdateTaskConfig{
		{GitURL: "https://github.com/u/r.git", Branch: "--upload-pack=id"},
		{GitURL: "ext::sh -c id", Branch: "main"},
		{GitURL: "file:///etc", Branch: "main"},
	} {
		if err := validateModpackSource(cfg); err == nil {
			t.Fatalf("expected rejection of %+v", cfg)
		}
	}
}

// TestExecutorRejectsHostileSourceBeforeGit is the end-to-end guarantee: a
// hostile branch/URL/subfolder must never reach a git subprocess. It installs
// spies in place of the clone/fetch seams and asserts they are never invoked.
func TestExecutorRejectsHostileSourceBeforeGit(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  ModpackUpdateTaskConfig
	}{
		{"branch option injection", ModpackUpdateTaskConfig{
			GitURL: "https://example.invalid/r.git",
			Branch: "--upload-pack=touch /tmp/pwned-branch",
		}},
		{"ext transport", ModpackUpdateTaskConfig{
			GitURL: "ext::sh -c id>/tmp/pwned-url",
			Branch: "main",
		}},
		{"file transport", ModpackUpdateTaskConfig{
			GitURL: "file:///etc",
			Branch: "main",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serverDir := t.TempDir()
			store := setupTestStore(t)
			defer store.Close()

			server := &storage.Server{
				ID: uuid.NewString(), Name: "s", DataPath: serverDir,
				Status: storage.StatusRunning,
			}
			ctx := context.Background()
			if err := store.CreateServer(ctx, server); err != nil {
				t.Fatalf("create server: %v", err)
			}
			s := NewScheduler(store, nil, nil, nil, nil, logger.New())

			cfgJSON, err := json.Marshal(tc.cfg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			task := &storage.ScheduledTask{
				ID: uuid.NewString(), ServerID: server.ID,
				Name: "hostile", TaskType: storage.TaskTypeModpackUpdate,
				Config: string(cfgJSON),
			}

			called := false
			origClone, origFetch := cloneFn, fetchFn
			t.Cleanup(func() { cloneFn, fetchFn = origClone, origFetch })
			cloneFn = func(context.Context, string, string, string) ([]byte, error) {
				called = true
				return nil, nil
			}
			fetchFn = func(context.Context, string, string) ([]byte, error) {
				called = true
				return nil, nil
			}

			if _, err := s.executeModpackUpdateTask(ctx, server, task); err == nil {
				t.Fatal("expected the executor to reject the hostile source")
			}
			if called {
				t.Fatal("git seam was invoked for a rejected source; validation did not run first")
			}
		})
	}
}

// TestExecutorRejectsEscapingSubfolder covers the traversal half end to end.
func TestExecutorRejectsEscapingSubfolder(t *testing.T) {
	serverDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s3cr3t"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	store := setupTestStore(t)
	defer store.Close()
	server := &storage.Server{
		ID: uuid.NewString(), Name: "s", DataPath: serverDir,
		Status: storage.StatusRunning,
	}
	ctx := context.Background()
	if err := store.CreateServer(ctx, server); err != nil {
		t.Fatalf("create server: %v", err)
	}
	s := NewScheduler(store, nil, nil, nil, nil, logger.New())

	cache := filepath.Join(serverDir, ".carbon-panel_modpack_git")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}

	// Relative traversal out of the cache into the sibling secret dir.
	rel, err := filepath.Rel(cache, outside)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}

	cfgJSON, _ := json.Marshal(ModpackUpdateTaskConfig{
		GitURL: "https://example.invalid/r.git", Branch: "main",
		TargetSubfolder: rel,
	})
	task := &storage.ScheduledTask{
		ID: uuid.NewString(), ServerID: server.ID, Name: "escape",
		TaskType: storage.TaskTypeModpackUpdate, Config: string(cfgJSON),
	}

	// Clone seam makes the cache look already-populated so execution proceeds
	// to the sync step, where the subfolder is resolved.
	origClone := cloneFn
	t.Cleanup(func() { cloneFn = origClone })
	cloneFn = func(context.Context, string, string, string) ([]byte, error) { return nil, nil }

	_, err = s.executeModpackUpdateTask(ctx, server, task)
	if err == nil {
		t.Fatal("expected rejection of escaping subfolder")
	}
	if _, statErr := os.Stat(filepath.Join(serverDir, "secret.txt")); statErr == nil {
		t.Fatal("host file outside the cache was copied into the server directory")
	}
}
