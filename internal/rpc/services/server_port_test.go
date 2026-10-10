package services

import (
	"context"
	"net"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/db"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestIsHostPortBound(t *testing.T) {
	// Find a free TCP port by listening on port 0
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on ephemeral port: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port

	// While ln is open, isHostPortBound(port) should be true
	if !isHostPortBound(port) {
		t.Errorf("expected isHostPortBound(%d) to be true while listener is active", port)
	}

	// Close listener and check again
	ln.Close()
	if isHostPortBound(port) {
		t.Errorf("expected isHostPortBound(%d) to be false after listener closed", port)
	}
}

func TestGetNextAvailablePort_SkipsHostBoundPorts(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := ctxWithRole("operator")

	// Bind port 25565 locally if free
	ln, err := net.Listen("tcp", "127.0.0.1:25565")
	if err != nil {
		t.Logf("could not bind 25565 directly in test: %v", err)
	} else {
		defer ln.Close()
	}

	resp, err := svc.GetNextAvailablePort(ctx, connect.NewRequest(&v1.GetNextAvailablePortRequest{}))
	if err != nil {
		t.Fatalf("GetNextAvailablePort failed: %v", err)
	}

	// If 25565 is bound, GetNextAvailablePort should return at least 25566
	if isHostPortBound(25565) && resp.Msg.Port == 25565 {
		t.Errorf("expected GetNextAvailablePort to skip bound port 25565, got %d", resp.Msg.Port)
	}
}

func TestCreateServer_RejectsHostBoundPort(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := ctxWithRole("operator")

	// Bind an ephemeral port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on ephemeral port: %v", err)
	}
	defer ln.Close()

	boundPort := ln.Addr().(*net.TCPAddr).Port

	// Attempt to create a server using that bound port
	_, err = svc.CreateServer(ctx, connect.NewRequest(&v1.CreateServerRequest{
		Name:        "port-conflict-server",
		NodeId:      "test-node",
		McVersion:   "1.20.1",
		DockerImage: "java21",
		Port:        int32(boundPort),
	}))
	if err == nil {
		t.Fatalf("expected CreateServer to reject bound port %d", boundPort)
	}

	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T: %v", err, err)
	}
	if connErr.Code() != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v (%v)", connErr.Code(), connErr)
	}
}

func TestStartServer_ReturnsErrorAndUpdatesStatusOnError(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	ctx := ctxWithRole("operator")

	// Create server in DB directly without container running
	server := &db.Server{
		ID:          "server-start-fail-test",
		Name:        "start-fail-test",
		NodeID:      "test-node",
		Port:        25599,
		Status:      db.StatusStopped,
		ContainerID: "non-existent-container",
	}
	if err := svc.store.CreateServer(context.Background(), server); err != nil {
		t.Fatalf("failed to seed test server: %v", err)
	}

	// Attempt to start server - should fail because test-node's docker daemon is mock/unreachable
	_, err := svc.StartServer(ctx, connect.NewRequest(&v1.StartServerRequest{
		Id: server.ID,
	}))
	if err == nil {
		t.Fatalf("expected StartServer to fail when container cannot be started")
	}

	// Verify server status in DB is marked as error
	updated, err := svc.store.GetServer(context.Background(), server.ID)
	if err != nil {
		t.Fatalf("failed to get server after start: %v", err)
	}
	if updated.Status != db.StatusError {
		t.Errorf("expected server status to be %s, got %s", db.StatusError, updated.Status)
	}
}

func TestParseModLoaderString(t *testing.T) {
	tests := []struct {
		input    string
		expected db.ModLoader
	}{
		{"MOD_LOADER_VANILLA", db.ModLoaderVanilla},
		{"vanilla", db.ModLoaderVanilla},
		{"VANILLA", db.ModLoaderVanilla},
		{"MOD_LOADER_FABRIC", db.ModLoaderFabric},
		{"fabric", db.ModLoaderFabric},
		{"MOD_LOADER_FORGE", db.ModLoaderForge},
		{"auto_curseforge", db.ModLoaderAutoCurseForge},
		{"MOD_LOADER_AUTO_CURSEFORGE", db.ModLoaderAutoCurseForge},
		{"", db.ModLoaderVanilla},
	}

	for _, tt := range tests {
		got := parseModLoaderString(tt.input)
		if got != tt.expected {
			t.Errorf("parseModLoaderString(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalizeModLoaderForType(t *testing.T) {
	tests := []struct {
		input    db.ModLoader
		expected string
	}{
		{db.ModLoaderVanilla, "VANILLA"},
		{db.ModLoader("MOD_LOADER_VANILLA"), "VANILLA"},
		{db.ModLoaderFabric, "FABRIC"},
		{db.ModLoader("MOD_LOADER_FABRIC"), "FABRIC"},
		{db.ModLoaderPaper, "PAPER"},
		{db.ModLoaderAutoCurseForge, "AUTO_CURSEFORGE"},
	}

	for _, tt := range tests {
		got := db.NormalizeModLoaderForType(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeModLoaderForType(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
