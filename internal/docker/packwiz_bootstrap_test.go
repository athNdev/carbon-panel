package docker

import (
	"slices"
	"testing"

	"github.com/athNdev/carbon-panel/internal/db"
)

func TestBuildEnvFromConfig_PackwizBootstrap(t *testing.T) {
	packURL := "http://carbon-panel.internal:8080/api/v1/packwiz/my-modpack/pack.toml"
	loader := "FABRIC"
	version := "1.20.1"

	cfg := &db.ServerConfig{
		PackwizURL: &packURL,
		Type:       &loader,
		Version:    &version,
	}

	env := buildEnvFromConfig(cfg)

	if !slices.Contains(env, "PACKWIZ_URL=http://carbon-panel.internal:8080/api/v1/packwiz/my-modpack/pack.toml") {
		t.Fatalf("expected PACKWIZ_URL to be populated in container env, got %v", env)
	}
	if !slices.Contains(env, "TYPE=FABRIC") {
		t.Fatalf("expected TYPE=FABRIC in container env, got %v", env)
	}
	if !slices.Contains(env, "VERSION=1.20.1") {
		t.Fatalf("expected VERSION=1.20.1 in container env, got %v", env)
	}
}
