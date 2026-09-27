package minecraft

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
)

func TestLoadYAMLConfig_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadYAMLConfig(dir, ConfigSpigot)
	if err == nil {
		t.Fatal("expected error for missing YAML file, got nil")
	}
}

func TestLoadYAMLConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "spigot.yml"), []byte(":\n\tbad: [unclosed"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadYAMLConfig(dir, ConfigSpigot)
	if err == nil {
		t.Fatal("expected parse error for invalid YAML, got nil")
	}
}

func TestLoadYAMLConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bukkit.yml"), []byte("settings:\n  allow-nether: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadYAMLConfig(dir, ConfigBukkit)
	if err != nil {
		t.Fatalf("LoadYAMLConfig: %v", err)
	}
	settings, ok := cfg["settings"].(map[string]any)
	if !ok {
		t.Fatalf("expected settings map, got %T", cfg["settings"])
	}
	if settings["allow-nether"] != true {
		t.Errorf("allow-nether = %v, want true", settings["allow-nether"])
	}
}

func TestSaveYAMLConfig_CreatesSubdirectory(t *testing.T) {
	dir := t.TempDir()
	in := map[string]any{
		"proxies": map[string]any{"velocity": map[string]any{"enabled": true}},
		"timeout": 30,
	}
	if err := SaveYAMLConfig(dir, ConfigPaperGlobal, in); err != nil {
		t.Fatalf("SaveYAMLConfig: %v", err)
	}

	path := filepath.Join(dir, "config", "paper-global.yml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}

	out, err := LoadYAMLConfig(dir, ConfigPaperGlobal)
	if err != nil {
		t.Fatalf("round-trip LoadYAMLConfig: %v", err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Errorf("round-trip mismatch:\n got: %#v\nwant: %#v", out, in)
	}
}

func TestSaveYAMLConfig_ErrorWhenPathNotDir(t *testing.T) {
	dir := t.TempDir()
	// Block config/ by creating a regular file in its place.
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveYAMLConfig(dir, ConfigPaperGlobal, map[string]any{"a": 1}); err == nil {
		t.Fatal("expected error when config path is a file, got nil")
	}
}

func TestGetAllConfigs_Spigot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte("max-players=20\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bukkit.yml"), []byte("settings:\n  allow-flight: false\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// spigot.yml intentionally missing: load error must be skipped silently.

	cfgs, err := GetAllConfigs(dir, models.ModLoaderSpigot)
	if err != nil {
		t.Fatalf("GetAllConfigs: %v", err)
	}

	if _, ok := cfgs["server.properties"]; !ok {
		t.Error("expected server.properties in configs")
	}
	if _, ok := cfgs["bukkit.yml"]; !ok {
		t.Error("expected bukkit.yml in configs")
	}
	if _, ok := cfgs["spigot.yml"]; ok {
		t.Error("spigot.yml missing on disk must not appear in configs")
	}
	if len(cfgs) != 2 {
		t.Errorf("len(configs) = %d, want 2", len(cfgs))
	}
}

func TestGetAllConfigs_MissingEverything(t *testing.T) {
	cfgs, err := GetAllConfigs(t.TempDir(), models.ModLoaderPaper)
	if err != nil {
		t.Fatalf("GetAllConfigs must never error: %v", err)
	}
	if len(cfgs) != 0 {
		t.Errorf("len(configs) = %d, want 0", len(cfgs))
	}
}

func TestGetAllConfigs_VanillaOnlyServerProperties(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte("motd=hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfgs, err := GetAllConfigs(dir, models.ModLoaderVanilla)
	if err != nil {
		t.Fatalf("GetAllConfigs: %v", err)
	}
	if len(cfgs) != 1 {
		t.Errorf("len(configs) = %d, want 1 (only server.properties)", len(cfgs))
	}
}
