package minecraft

import (
	"os"
	"path/filepath"
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
)

func TestLoadServerProperties(t *testing.T) {
	dir := t.TempDir()
	content := "# comment line\n\nserver-port=25565\nmotd = Hello World\nempty=\nno-equals-here\n"
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	props, err := LoadServerProperties(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if props["server-port"] != "25565" || props["motd"] != "Hello World" {
		t.Errorf("props = %v", props)
	}
	if v, ok := props["empty"]; !ok || v != "" {
		t.Errorf("empty value = %q, %v", v, ok)
	}
	if _, ok := props["no-equals-here"]; ok {
		t.Error("line without = should be skipped")
	}

	if _, err := LoadServerProperties(t.TempDir()); err == nil {
		t.Error("missing file: want error")
	}
}

func TestSaveLoadServerProperties_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := ServerProperties{"server-port": "25566", "motd": "Round Trip"}
	if err := SaveServerProperties(dir, want); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	got, err := LoadServerProperties(dir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if got["server-port"] != "25566" || got["motd"] != "Round Trip" {
		t.Errorf("round-trip = %v", got)
	}
}

func TestIsValidModFile_Extensions(t *testing.T) {
	if !IsValidModFile("sodium.jar", models.ModLoaderForge) {
		t.Error("jar should be valid for forge")
	}
	if IsValidModFile("readme.txt", models.ModLoaderForge) {
		t.Error("txt should be invalid for forge")
	}
	if IsValidModFile("MOD.JAR", models.ModLoaderFabric) {
		t.Log("uppercase extension accepted (case-insensitive)")
	}
	if IsValidModFile("anything.xyz", models.ModLoaderVanilla) {
		t.Error("vanilla takes no mod files")
	}
}

func TestMatchModLoader_ExactAndGarbage(t *testing.T) {
	if loader, ok := MatchModLoader("paper"); !ok || loader != models.ModLoaderPaper {
		t.Errorf("match paper = %q, %v", loader, ok)
	}
	if _, ok := MatchModLoader("definitely-not-a-loader-xyz"); ok {
		t.Error("garbage input matched")
	}
}

func TestDetectModpackLoader_Candidates(t *testing.T) {
	if loader, ok := DetectModpackLoader("forge-47.2.0", "1.20.1"); !ok {
		t.Errorf("forge loader not detected: %q", loader)
	} else if loader != models.ModLoaderForge {
		t.Errorf("detected = %q, want forge", loader)
	}
	if _, ok := DetectModpackLoader("1.20.1", "vanilla-ish"); ok {
		t.Log("version-only input matched a loader (fuzzy)")
	}
}

func TestGetModsPath_PerLoader(t *testing.T) {
	if got := GetModsPath("/data", models.ModLoaderForge); got == "" {
		t.Error("forge mods path empty")
	}
	if got := GetModsPath("/data", models.ModLoaderVanilla); got != "" {
		t.Errorf("vanilla mods path = %q, want empty", got)
	}
}
