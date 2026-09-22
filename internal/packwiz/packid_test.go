package packwiz

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

func TestValidPackID(t *testing.T) {
	valid := []string{"abc", "pack-1", "A_b.c", "550e8400-e29b-41d4-a716-446655440000"}
	for _, id := range valid {
		if err := validPackID(id); err != nil {
			t.Errorf("validPackID(%q) = %v, want nil", id, err)
		}
	}
	invalid := []string{"", ".", "..", "a/b", `a\b`, "../x", "x/../../y", "id with space", "id:colon"}
	for _, id := range invalid {
		if err := validPackID(id); err == nil {
			t.Errorf("validPackID(%q) = nil, want error", id)
		}
	}
}

// TestPackIDCannotEscapeBaseDir is the security regression test: the pack id is
// a directory name and arrives from a request body (POST /api/v1/packwiz/packs),
// so a traversal id must be refused rather than creating files outside the
// packwiz base directory.
func TestPackIDCannotEscapeBaseDir(t *testing.T) {
	base := t.TempDir()
	m := NewManager(filepath.Join(base, "packs"), logger.New())
	outside := filepath.Join(base, "pwned-pack")

	if _, err := m.CreatePack(&Pack{ID: "../../pwned-pack", Name: "pwned"}); err == nil {
		t.Error("CreatePack accepted a traversal id")
	}
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Fatalf("pack id escaped the base dir: %s was created", outside)
	}

	if _, err := m.GetPack(".."); err == nil {
		t.Error("GetPack accepted a traversal id")
	}
	if err := m.DeletePack("../.."); err == nil {
		t.Error("DeletePack accepted a traversal id")
	}
	if err := m.UpdatePack(&Pack{ID: "../escape", Name: "x"}); err == nil {
		t.Error("UpdatePack accepted a traversal id")
	}
	if _, _, err := m.ServePackFile("..", "pack.toml"); err == nil {
		t.Error("ServePackFile accepted a traversal id")
	}

	// A legitimate id must still work end to end.
	created, err := m.CreatePack(&Pack{ID: "valid-pack_1.0", Name: "ok"})
	if err != nil {
		t.Fatalf("CreatePack rejected a valid id: %v", err)
	}
	if _, err := m.GetPack(created.ID); err != nil {
		t.Fatalf("GetPack failed for a valid id: %v", err)
	}
}
