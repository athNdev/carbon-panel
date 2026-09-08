package minecraft

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateVelocitySecret(t *testing.T) {
	secret1, err := GenerateVelocitySecret()
	if err != nil {
		t.Fatalf("GenerateVelocitySecret failed: %v", err)
	}
	if len(secret1) != 64 {
		t.Fatalf("expected 64 hex characters, got %d (%s)", len(secret1), secret1)
	}

	secret2, err := GenerateVelocitySecret()
	if err != nil {
		t.Fatalf("GenerateVelocitySecret 2 failed: %v", err)
	}
	if secret1 == secret2 {
		t.Errorf("expected unique secrets, got duplicate: %s", secret1)
	}
}

func TestSyncVelocitySecretToDataPath_ModernGlobal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "paper-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	secret := "abcd1234efgh5678"
	targetFile, err := SyncVelocitySecretToDataPath(tmpDir, secret)
	if err != nil {
		t.Fatalf("SyncVelocitySecretToDataPath failed: %v", err)
	}
	if targetFile != "config/paper-global.yml" {
		t.Errorf("expected target config/paper-global.yml, got %s", targetFile)
	}

	cfg, err := LoadYAMLConfig(tmpDir, ConfigPaperGlobal)
	if err != nil {
		t.Fatalf("LoadYAMLConfig failed: %v", err)
	}

	proxies := cfg["proxies"].(map[string]any)
	velocity := proxies["velocity"].(map[string]any)
	if velocity["secret"] != secret {
		t.Errorf("expected secret %s, got %v", secret, velocity["secret"])
	}
	if velocity["enabled"] != true {
		t.Errorf("expected enabled true")
	}
}

func TestSyncVelocitySecretToDataPath_LegacyPaper(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "paper-legacy-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create legacy paper.yml
	legacyPath := filepath.Join(tmpDir, "paper.yml")
	if err := os.WriteFile(legacyPath, []byte("settings:\n  other: 123\n"), 0644); err != nil {
		t.Fatal(err)
	}

	secret := "secret-legacy-999"
	targetFile, err := SyncVelocitySecretToDataPath(tmpDir, secret)
	if err != nil {
		t.Fatalf("SyncVelocitySecretToDataPath failed: %v", err)
	}
	if targetFile != "paper.yml" {
		t.Errorf("expected target paper.yml, got %s", targetFile)
	}

	cfg, err := LoadYAMLConfig(tmpDir, ConfigPaper)
	if err != nil {
		t.Fatalf("LoadYAMLConfig failed: %v", err)
	}

	settings := cfg["settings"].(map[string]any)
	velocity := settings["velocity-support"].(map[string]any)
	if velocity["secret"] != secret {
		t.Errorf("expected secret %s, got %v", secret, velocity["secret"])
	}
}
