package svc

import (
	"strings"
	"testing"
)

func TestProperties_ParseAndSerialize(t *testing.T) {
	input := `# Minecraft server properties
# Fri Sep 25 00:55:47 UTC 2026
enable-jmx-monitoring=false
rcon.port=25575
level-seed=
gamemode=survival
enable-query=false
generator-settings={}
enforce-secure-profile=true
server-port=25565
pvp=true
motd=A Minecraft Server
`

	props := ParseProperties(input)
	if val, ok := props.Get("motd"); !ok || val != "A Minecraft Server" {
		t.Fatalf("expected motd 'A Minecraft Server', got '%s', ok=%v", val, ok)
	}
	if val, ok := props.Get("server-port"); !ok || val != "25565" {
		t.Fatalf("expected server-port '25565', got '%s', ok=%v", val, ok)
	}

	// Update existing property
	props.Set("motd", "Carbon Cloud High-Performance Node")
	props.Set("difficulty", "hard") // Add new property

	serialized := props.Serialize()

	// Verify comments preserved
	if !strings.Contains(serialized, "# Minecraft server properties") {
		t.Fatalf("expected comment preserved, got:\n%s", serialized)
	}
	if !strings.Contains(serialized, "motd=Carbon Cloud High-Performance Node") {
		t.Fatalf("expected updated motd, got:\n%s", serialized)
	}
	if !strings.Contains(serialized, "difficulty=hard") {
		t.Fatalf("expected added difficulty, got:\n%s", serialized)
	}

	// Verify delete
	deleted := props.Delete("rcon.port")
	if !deleted {
		t.Fatalf("expected rcon.port deleted")
	}
	if _, ok := props.Get("rcon.port"); ok {
		t.Fatalf("expected rcon.port to be deleted")
	}

	serializedAfterDelete := props.Serialize()
	if strings.Contains(serializedAfterDelete, "rcon.port") {
		t.Fatalf("expected rcon.port removed from serialized output")
	}
}
