package minecraft

import (
	"testing"

	models "github.com/nickheyer/discopanel/internal/db"
)

func TestGetModLoaderInfo_Folia(t *testing.T) {
	info := GetModLoaderInfo(models.ModLoaderFolia)

	if info.Name != string(models.ModLoaderFolia) {
		t.Errorf("expected Name %q, got %q", models.ModLoaderFolia, info.Name)
	}
	if info.DisplayName != "Folia" {
		t.Errorf("expected DisplayName %q, got %q", "Folia", info.DisplayName)
	}
	expectedDesc := "Regionized multi-threaded server software fork of Paper"
	if info.Description != expectedDesc {
		t.Errorf("expected Description %q, got %q", expectedDesc, info.Description)
	}
	if info.Category != "Paper" {
		t.Errorf("expected Category %q, got %q", "Paper", info.Category)
	}
	if info.ModsDirectory != "plugins" {
		t.Errorf("expected ModsDirectory %q, got %q", "plugins", info.ModsDirectory)
	}
}

func TestGetModLoaderInfo_Pufferfish(t *testing.T) {
	info := GetModLoaderInfo(models.ModLoaderPufferfish)

	if info.DisplayName != "Pufferfish" {
		t.Errorf("expected DisplayName %q, got %q", "Pufferfish", info.DisplayName)
	}
	expectedDesc := "Performance-focused fork of Paper"
	if info.Description != expectedDesc {
		t.Errorf("expected Description %q, got %q", expectedDesc, info.Description)
	}
}

func TestGetAllModLoaders(t *testing.T) {
	loaders := GetAllModLoaders()
	foundFolia := false
	foundPufferfish := false

	for _, loader := range loaders {
		if loader.Name == string(models.ModLoaderFolia) {
			foundFolia = true
			if loader.DisplayName != "Folia" {
				t.Errorf("GetAllModLoaders Folia DisplayName = %q, want %q", loader.DisplayName, "Folia")
			}
		}
		if loader.Name == string(models.ModLoaderPufferfish) {
			foundPufferfish = true
			if loader.DisplayName != "Pufferfish" {
				t.Errorf("GetAllModLoaders Pufferfish DisplayName = %q, want %q", loader.DisplayName, "Pufferfish")
			}
		}
	}

	if !foundFolia {
		t.Error("GetAllModLoaders missing Folia")
	}
	if !foundPufferfish {
		t.Error("GetAllModLoaders missing Pufferfish")
	}
}

func TestIsValidModFile(t *testing.T) {
	tests := []struct {
		filename string
		loader   models.ModLoader
		expected bool
	}{
		{"plugin.jar", models.ModLoaderFolia, true},
		{"plugin.JAR", models.ModLoaderFolia, true},
		{"plugin.zip", models.ModLoaderFolia, false},
		{"mod.jar", models.ModLoaderForge, true},
		{"mod.jar", models.ModLoaderFabric, true},
		{"mod.jar", models.ModLoaderVanilla, false},
	}

	for _, tt := range tests {
		got := IsValidModFile(tt.filename, tt.loader)
		if got != tt.expected {
			t.Errorf("IsValidModFile(%q, %q) = %v, want %v", tt.filename, tt.loader, got, tt.expected)
		}
	}
}

func TestMatchModLoader(t *testing.T) {
	loader, ok := MatchModLoader("folia")
	if !ok {
		t.Fatal("expected MatchModLoader('folia') to match")
	}
	if loader != models.ModLoaderFolia {
		t.Errorf("expected %v, got %v", models.ModLoaderFolia, loader)
	}
}
