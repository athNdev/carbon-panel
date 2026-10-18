package services

import (
	"context"
	"fmt"
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

func TestCreateServer_RejectsMemoryRequestBeyondDockerHostCapacity(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	svc.hostMemoryMB = func(context.Context, string) (int64, error) { return 4096, nil }

	_, err := svc.CreateServer(ctxWithRole("operator"), connect.NewRequest(&v1.CreateServerRequest{
		Name:        "oversized-server",
		NodeId:      "test-node",
		McVersion:   "1.20.1",
		DockerImage: "java21",
		Port:        25601,
		Memory:      6144,
	}))
	if err == nil {
		t.Fatal("expected memory request larger than Docker host capacity to be rejected")
	}
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T: %v", err, err)
	}
	if connErr.Code() != connect.CodeResourceExhausted {
		t.Fatalf("expected CodeResourceExhausted, got %v (%v)", connErr.Code(), connErr)
	}
	servers, listErr := svc.store.ListServers(context.Background())
	if listErr != nil {
		t.Fatalf("failed to list servers: %v", listErr)
	}
	if len(servers) != 0 {
		t.Fatalf("oversized request persisted %d servers before rejecting", len(servers))
	}
}

func TestCreateServer_SerializesMemoryReservationAgainstConcurrentRequests(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	svc.hostMemoryMB = func(context.Context, string) (int64, error) { return 4096, nil }
	ctx := ctxWithRole("operator")
	type result struct{ err error }
	results := make(chan result, 2)
	for i, port := range []int32{25603, 25604} {
		go func(i int, port int32) {
			_, err := svc.CreateServer(ctx, connect.NewRequest(&v1.CreateServerRequest{
				Name:        fmt.Sprintf("capacity-race-%d", i),
				NodeId:      "test-node",
				McVersion:   "1.20.1",
				DockerImage: "java21",
				Port:        port,
				Memory:      3072,
			}))
			results <- result{err: err}
		}(i, port)
	}

	accepted, exhausted := 0, 0
	for range 2 {
		result := <-results
		if result.err == nil {
			accepted++
			continue
		}
		connErr, ok := result.err.(*connect.Error)
		if !ok || connErr.Code() != connect.CodeResourceExhausted {
			t.Fatalf("unexpected create result: %v", result.err)
		}
		exhausted++
	}
	if accepted != 1 || exhausted != 1 {
		t.Fatalf("accepted=%d exhausted=%d, want one of each", accepted, exhausted)
	}
	servers, err := svc.store.ListServers(context.Background())
	if err != nil {
		t.Fatalf("failed to list servers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("stored %d servers after capacity race, want 1", len(servers))
	}
}

func TestCreateServer_AutoPlacementSkipsNodeWithoutPhysicalMemoryCapacity(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	testNode, err := svc.store.GetNode(context.Background(), "test-node")
	if err != nil {
		t.Fatalf("failed to load helper node: %v", err)
	}
	testNode.MaxMemoryMB = 1
	if err := svc.store.UpdateNode(context.Background(), testNode); err != nil {
		t.Fatalf("failed to cap helper node: %v", err)
	}
	for _, node := range []*db.Node{
		{ID: "a-small-node", Name: "Small Node", Host: "tcp://127.0.0.1:2375", Enabled: true, Status: db.NodeStatusOnline},
		{ID: "z-large-node", Name: "Large Node", Host: "tcp://127.0.0.1:2375", Enabled: true, Status: db.NodeStatusOnline},
	} {
		if err := svc.store.CreateNode(context.Background(), node); err != nil {
			t.Fatalf("failed to seed node %s: %v", node.ID, err)
		}
	}
	if err := svc.store.CreateServer(context.Background(), &db.Server{
		ID: "large-node-reservation", Name: "large-node-reservation", NodeID: "z-large-node",
		Port: 25607, Memory: 1024, Status: db.StatusStopped,
	}); err != nil {
		t.Fatalf("failed to seed existing reservation: %v", err)
	}
	svc.hostMemoryMB = func(_ context.Context, nodeID string) (int64, error) {
		if nodeID == "a-small-node" {
			return 4096, nil
		}
		return 16 * 1024, nil
	}

	resp, err := svc.CreateServer(ctxWithRole("operator"), connect.NewRequest(&v1.CreateServerRequest{
		Name:        "large-modpack",
		McVersion:   "1.20.1",
		DockerImage: "java21",
		Port:        25608,
		Memory:      6144,
	}))
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if resp.Msg.Server.NodeId != "z-large-node" {
		t.Fatalf("server placed on %q, want z-large-node with sufficient physical capacity", resp.Msg.Server.NodeId)
	}
}

func TestStartServer_RejectsServerWhenNodeMemoryIsOvercommitted(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	svc.hostMemoryMB = func(context.Context, string) (int64, error) { return 4096, nil }
	server := &db.Server{
		ID:          "oversized-start-test",
		Name:        "oversized-start-test",
		NodeID:      "test-node",
		Port:        25602,
		Memory:      6144,
		Status:      db.StatusStopped,
		ContainerID: "must-not-start",
	}
	if err := svc.store.CreateServer(context.Background(), server); err != nil {
		t.Fatalf("failed to create server fixture: %v", err)
	}

	_, err := svc.StartServer(ctxWithRole("operator"), connect.NewRequest(&v1.StartServerRequest{Id: server.ID}))
	if err == nil {
		t.Fatal("expected start to be rejected when reservations exceed Docker host capacity")
	}
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T: %v", err, err)
	}
	if connErr.Code() != connect.CodeResourceExhausted {
		t.Fatalf("expected CodeResourceExhausted, got %v (%v)", connErr.Code(), connErr)
	}
	updated, getErr := svc.store.GetServer(context.Background(), server.ID)
	if getErr != nil {
		t.Fatalf("failed to reload server: %v", getErr)
	}
	if updated.Status != db.StatusStopped {
		t.Fatalf("rejected start changed status to %q", updated.Status)
	}
}

func TestUpdateServer_RejectsMemoryIncreaseBeyondNodeCapacity(t *testing.T) {
	svc, _ := newDockerPrivilegeTestService(t)
	svc.hostMemoryMB = func(context.Context, string) (int64, error) { return 4096, nil }
	server := &db.Server{
		ID:     "oversized-update-test",
		Name:   "oversized-update-test",
		NodeID: "test-node",
		Port:   25605,
		Memory: 2048,
		Status: db.StatusStopped,
	}
	if err := svc.store.CreateServer(context.Background(), server); err != nil {
		t.Fatalf("failed to create server fixture: %v", err)
	}

	_, err := svc.UpdateServer(ctxWithRole("operator"), connect.NewRequest(&v1.UpdateServerRequest{
		Id:     server.ID,
		Memory: 6144,
	}))
	if err == nil {
		t.Fatal("expected memory increase beyond node capacity to be rejected")
	}
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T: %v", err, err)
	}
	if connErr.Code() != connect.CodeResourceExhausted {
		t.Fatalf("expected CodeResourceExhausted, got %v (%v)", connErr.Code(), connErr)
	}
	updated, getErr := svc.store.GetServer(context.Background(), server.ID)
	if getErr != nil {
		t.Fatalf("failed to reload server: %v", getErr)
	}
	if updated.Memory != server.Memory {
		t.Fatalf("rejected update changed memory from %d to %d", server.Memory, updated.Memory)
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
