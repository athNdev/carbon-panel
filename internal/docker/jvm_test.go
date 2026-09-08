package docker

import (
	"strings"
	"testing"

	"github.com/nickheyer/discopanel/internal/db"
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
