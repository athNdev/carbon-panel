package minecraft

import (
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
)

func TestGetConfigFiles_PerLoader(t *testing.T) {
	vanilla := GetConfigFiles(models.ModLoaderVanilla)
	if len(vanilla) != 1 {
		t.Errorf("vanilla = %d files", len(vanilla))
	}
	paper := GetConfigFiles(models.ModLoaderPaper)
	if len(paper) <= len(vanilla) {
		t.Errorf("paper should list more files than vanilla: %d", len(paper))
	}
	for _, loader := range []models.ModLoader{
		models.ModLoaderForge, models.ModLoaderFabric, models.ModLoaderNeoForge,
		models.ModLoader("unknown-loader"),
	} {
		files := GetConfigFiles(loader)
		if len(files) != 1 {
			t.Errorf("loader %q = %d files, want 1 (server.properties)", loader, len(files))
		}
	}
}

func TestServerProperties_Accessors(t *testing.T) {
	p := ServerProperties{"port": "25565", "motd": "Hi", "online": "true", "bad-int": "abc"}

	if got := p.GetString("motd", "dflt"); got != "Hi" {
		t.Errorf("string = %q", got)
	}
	if got := p.GetString("missing", "dflt"); got != "dflt" {
		t.Errorf("missing string = %q", got)
	}
	if got := p.GetInt("port", 0); got != 25565 {
		t.Errorf("int = %d", got)
	}
	if got := p.GetInt("bad-int", 7); got != 7 {
		t.Errorf("bad int = %d, want default", got)
	}
	if got := p.GetInt("missing", 7); got != 7 {
		t.Errorf("missing int = %d", got)
	}
	if !p.GetBool("online", false) {
		t.Error("bool true not read")
	}
	if p.GetBool("motd", false) {
		t.Error("non-true string read as true")
	}
	if !p.GetBool("missing", true) {
		t.Error("missing bool default not used")
	}

	p.SetString("motd", "Yo")
	p.SetInt("port", 25566)
	p.SetBool("online", false)
	if p["motd"] != "Yo" || p["port"] != "25566" || p["online"] != "false" {
		t.Errorf("after set = %v", p)
	}
}
