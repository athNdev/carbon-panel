package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRecoveryKeyPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")

	path, err := writeRecoveryKey(dir, "s3cr3t")
	if err != nil {
		t.Fatalf("writeRecoveryKey: %v", err)
	}
	if want := filepath.Join(dir, recoveryKeyFileName); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("perm = %04o, want 0600", perm)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "s3cr3t" {
		t.Fatalf("contents = %q, want %q", got, "s3cr3t")
	}
}

// The whole point of the change: a default start must not leak the secret into
// whatever captures stdout/stderr.
func TestAnnounceRecoveryKeyDoesNotPrintSecretByDefault(t *testing.T) {
	var buf bytes.Buffer

	announceRecoveryKey(&buf, "s3cr3t", "/data/recovery.key", false)

	out := buf.String()
	if strings.Contains(out, "s3cr3t") {
		t.Fatalf("secret leaked to stderr:\n%s", out)
	}
	if !strings.Contains(out, "/data/recovery.key") {
		t.Fatalf("operator cannot find the key, output was:\n%s", out)
	}
}

func TestAnnounceRecoveryKeyPrintsSecretWhenAllowed(t *testing.T) {
	var buf bytes.Buffer

	announceRecoveryKey(&buf, "s3cr3t", "/data/recovery.key", true)

	if !strings.Contains(buf.String(), "s3cr3t") {
		t.Fatalf("opt-in print produced no secret:\n%s", buf.String())
	}
}

func TestAnnounceRecoveryKeySilentWithoutKey(t *testing.T) {
	var buf bytes.Buffer

	announceRecoveryKey(&buf, "", "/data/recovery.key", true)

	if buf.Len() != 0 {
		t.Fatalf("expected no output for empty key, got:\n%s", buf.String())
	}
}

func TestRecoveryKeyPrintAllowed(t *testing.T) {
	tests := []struct {
		name     string
		explicit bool
		env      map[string]string
		want     bool
	}{
		{name: "default is off", want: false},
		{name: "flag opts in", explicit: true, want: true},
		{name: "env true opts in", env: map[string]string{recoveryKeyEnv: "true"}, want: true},
		{name: "env 1 opts in", env: map[string]string{recoveryKeyEnv: "1"}, want: true},
		{name: "env false stays off", env: map[string]string{recoveryKeyEnv: "false"}, want: false},
		// Fail closed: a typo must never silently start leaking the secret.
		{name: "unparseable env fails closed", env: map[string]string{recoveryKeyEnv: "yes-please"}, want: false},
		{name: "empty env fails closed", env: map[string]string{recoveryKeyEnv: ""}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(k string) (string, bool) {
				v, ok := tt.env[k]
				return v, ok
			}
			if got := recoveryKeyPrintAllowed(tt.explicit, lookup); got != tt.want {
				t.Fatalf("recoveryKeyPrintAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}
