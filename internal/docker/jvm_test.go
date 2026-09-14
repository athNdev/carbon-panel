package docker

import (
	"strings"
	"testing"

	"github.com/athNdev/mineserver/internal/db"
)

func TestBuildEnvFromConfig_GenerationalZGC(t *testing.T) {
	trueVal := true
	cfg := &db.ServerConfig{
		UseAikarFlags:      &trueVal,
		UseGenerationalZgc: &trueVal,
	}

	env := buildEnvFromConfig(cfg)

	var useAikar, jvmXx string
	for _, e := range env {
		if strings.HasPrefix(e, "USE_AIKAR_FLAGS=") {
			useAikar = strings.TrimPrefix(e, "USE_AIKAR_FLAGS=")
		}
		if strings.HasPrefix(e, "JVM_XX_OPTS=") {
			jvmXx = strings.TrimPrefix(e, "JVM_XX_OPTS=")
		}
	}

	if useAikar != "false" {
		t.Fatalf("expected USE_AIKAR_FLAGS=false when ZGC is active, got %s", useAikar)
	}
	if !strings.Contains(jvmXx, "-XX:+UseZGC -XX:+ZGenerational") {
		t.Fatalf("expected Generational ZGC flags in JVM_XX_OPTS, got %s", jvmXx)
	}
}

func TestBuildEnvFromConfig_CurseForgeDefaultsAndExtraction(t *testing.T) {
	pageURL := "https://www.curseforge.com/minecraft/modpacks/all-the-mods-9/files/5016013"
	cfg := &db.ServerConfig{
		CFPageURL: &pageURL,
	}

	env := buildEnvFromConfig(cfg)

	var slug, fileID, parallel string
	for _, e := range env {
		if strings.HasPrefix(e, "CF_SLUG=") {
			slug = strings.TrimPrefix(e, "CF_SLUG=")
		}
		if strings.HasPrefix(e, "CF_FILE_ID=") {
			fileID = strings.TrimPrefix(e, "CF_FILE_ID=")
		}
		if strings.HasPrefix(e, "CF_PARALLEL_DOWNLOADS=") {
			parallel = strings.TrimPrefix(e, "CF_PARALLEL_DOWNLOADS=")
		}
	}

	if slug != "all-the-mods-9" {
		t.Errorf("expected CF_SLUG to be 'all-the-mods-9', got %q", slug)
	}
	if fileID != "5016013" {
		t.Errorf("expected CF_FILE_ID to be '5016013', got %q", fileID)
	}
	if parallel != "4" {
		t.Errorf("expected default CF_PARALLEL_DOWNLOADS to be '4', got %q", parallel)
	}
}
