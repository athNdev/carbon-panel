package sftp

import (
	"testing"
)

func TestJailPath(t *testing.T) {
	root := "/data/server"
	got, err := JailPath(root, "world/level.dat")
	if err != nil || got != "/data/server/world/level.dat" {
		t.Fatalf("got %s err %v", got, err)
	}
	if _, err := JailPath(root, "../etc/passwd"); err == nil {
		t.Fatal("expected escape error")
	}
}
