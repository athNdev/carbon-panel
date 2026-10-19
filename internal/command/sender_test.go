// Merged in the MINE-172 integration: this file existed independently on
// both lines of development, so the union of both test sets is kept.

package command

import (
	"context"
	"errors"
	"fmt"
	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"strings"
	"testing"
)

type fakeExecutor struct {
	output string
	err    error
	calls  int
	lastID string
}

func (f *fakeExecutor) ExecCommand(_ context.Context, containerID string, _ string) (string, error) {
	f.calls++
	f.lastID = containerID
	return f.output, f.err
}

func newSenderTestStore(t *testing.T) *storage.Store {
	t.Helper()

	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", dbName),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return store
}

func TestSendCommand_MissingServer(t *testing.T) {
	s := NewSender(newSenderTestStore(t), &config.Config{}, &fakeExecutor{}, nil)

	if _, err := s.SendCommand(context.Background(), "nope", "list"); err == nil {
		t.Error("missing server: want error")
	}
}

func TestSendCommand_ServerWithoutContainer(t *testing.T) {
	store := newSenderTestStore(t)
	ctx := context.Background()

	if err := store.DB().Create(&storage.Server{ID: "srv-noc", Name: "noc", DataPath: t.TempDir()}).Error; err != nil {
		t.Fatalf("failed to insert server: %v", err)
	}
	s := NewSender(store, &config.Config{}, &fakeExecutor{}, nil)

	if _, err := s.SendCommand(ctx, "srv-noc", "list"); err == nil {
		t.Error("containerless server: want error")
	}
}

func TestSendCommand_RCONDisabledFallsBackToExec(t *testing.T) {
	store := newSenderTestStore(t)
	ctx := context.Background()

	if err := store.DB().Create(&storage.Server{ID: "srv-1", Name: "s1", DataPath: t.TempDir(), ContainerID: "cid-1"}).Error; err != nil {
		t.Fatalf("failed to insert server: %v", err)
	}
	disabled := false
	if err := store.DB().Create(&storage.ServerConfig{ServerID: "srv-1", EnableRCON: &disabled}).Error; err != nil {
		t.Fatalf("failed to insert config: %v", err)
	}

	exec := &fakeExecutor{output: "exec-output"}
	s := NewSender(store, &config.Config{}, exec, nil)

	out, err := s.SendCommand(ctx, "srv-1", "list")
	if err != nil {
		t.Fatalf("fallback exec failed: %v", err)
	}
	if out != "exec-output" || exec.calls != 1 || exec.lastID != "cid-1" {
		t.Errorf("exec = %q calls=%d id=%q", out, exec.calls, exec.lastID)
	}
}

func TestResolveExecutor_DefaultWithoutPool(t *testing.T) {
	exec := &fakeExecutor{}
	s := NewSender(newSenderTestStore(t), &config.Config{}, exec, nil)

	if got := s.resolveExecutor("any-node"); got != DockerExecutor(exec) {
		t.Error("nil pool must resolve to the default executor")
	}
}

func TestResolveExecutor_PoolFallsBackToDefault(t *testing.T) {
	store := newSenderTestStore(t)
	ctx := context.Background()

	if err := store.DB().Create(&storage.Server{ID: "srv-pool", Name: "p", DataPath: t.TempDir(), ContainerID: "cid-9"}).Error; err != nil {
		t.Fatalf("failed to insert server: %v", err)
	}
	disabled := false
	if err := store.DB().Create(&storage.ServerConfig{ServerID: "srv-pool", EnableRCON: &disabled}).Error; err != nil {
		t.Fatalf("failed to insert config: %v", err)
	}

	exec := &fakeExecutor{output: "pool-fallback"}
	pool := docker.NewClientPool(store, nil, logger.New())
	s := NewSender(store, &config.Config{}, exec, pool)

	out, err := s.SendCommand(ctx, "srv-pool", "list")
	if err != nil {
		t.Fatalf("fallback send failed: %v", err)
	}
	if out != "pool-fallback" || exec.calls != 1 || exec.lastID != "cid-9" {
		t.Errorf("exec = %q calls=%d id=%q", out, exec.calls, exec.lastID)
	}
}

// --- tests present only on the pre-merge line of development ---

func testStore(t *testing.T) *storage.Store {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	store, err := storage.NewSQLiteStore(&config.Config{
		Database: config.DatabaseConfig{
			Path:           fmt.Sprintf("file:%s?mode=memory&cache=shared", name),
			AutoMigrate:    true,
			MaxConnections: 5,
		},
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func testSender(store *storage.Store, exec DockerExecutor, global map[string]any) *Sender {
	cfg := &config.Config{}
	cfg.Minecraft.GlobalConfig = global
	cfg.Docker.NetworkName = "test-net"
	return NewSender(store, cfg, exec, nil)
}

func makeServer(t *testing.T, store *storage.Store, id, container string) {
	t.Helper()
	if err := store.CreateServer(context.Background(), &storage.Server{
		ID: id, Name: id, ContainerID: container, DataPath: "/tmp/" + id,
	}); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
}

func TestSendCommandUnknownServer(t *testing.T) {
	s := testSender(testStore(t), &fakeExecutor{}, nil)
	if _, err := s.SendCommand(context.Background(), "nope", "list"); err == nil {
		t.Fatal("want error")
	}
}

func TestSendCommandNoContainer(t *testing.T) {
	store := testStore(t)
	makeServer(t, store, "s1", "")
	s := testSender(store, &fakeExecutor{}, nil)
	if _, err := s.SendCommand(context.Background(), "s1", "list"); err == nil {
		t.Fatal("want error")
	}
}

func TestSendCommandMissingConfigFallsBackToExec(t *testing.T) {
	store := testStore(t)
	makeServer(t, store, "s1", "c1")
	exec := &fakeExecutor{output: "exec-out"}
	s := testSender(store, exec, nil)
	out, err := s.SendCommand(context.Background(), "s1", "list")
	if err != nil || out != "exec-out" || exec.calls != 1 {
		t.Fatalf("out=%q err=%v calls=%d", out, err, exec.calls)
	}
}

func TestSendCommandRCONDisabledFallsBack(t *testing.T) {
	store := testStore(t)
	makeServer(t, store, "s1", "c1")
	off := false
	if err := store.SaveServerConfig(context.Background(), &storage.ServerConfig{
		ID: "cfg1", ServerID: "s1", EnableRCON: &off,
	}); err != nil {
		t.Fatalf("SaveServerConfig: %v", err)
	}
	exec := &fakeExecutor{output: "fallback-out"}
	s := testSender(store, exec, nil)
	out, err := s.SendCommand(context.Background(), "s1", "list")
	if err != nil || out != "fallback-out" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestSendCommandRCONPortParsing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		global map[string]any
	}{
		{"int", map[string]any{"rconPort": 25575}},
		{"int64", map[string]any{"rconPort": int64(25575)}},
		{"float", map[string]any{"rconPort": float64(25575)}},
		{"string", map[string]any{"rconPort": "25575"}},
		{"bad-string", map[string]any{"rconPort": "nope"}},
		{"absent", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := testStore(t)
			makeServer(t, store, "s1", "c1")
			on := true
			if err := store.SaveServerConfig(context.Background(), &storage.ServerConfig{
				ID: "cfg1", ServerID: "s1", EnableRCON: &on,
			}); err != nil {
				t.Fatalf("SaveServerConfig: %v", err)
			}
			exec := &fakeExecutor{output: "ok"}
			s := testSender(store, exec, tc.global)
			// No daemon in test env: container-IP resolution fails and the
			// sender falls back to docker exec for every port shape.
			out, err := s.SendCommand(context.Background(), "s1", "list")
			if err != nil || out != "ok" || exec.calls != 1 {
				t.Fatalf("out=%q err=%v calls=%d", out, err, exec.calls)
			}
		})
	}
}

func TestSendCommandExecErrorPropagates(t *testing.T) {
	store := testStore(t)
	makeServer(t, store, "s1", "c1")
	exec := &fakeExecutor{err: errors.New("boom")}
	s := testSender(store, exec, nil)
	if _, err := s.SendCommand(context.Background(), "s1", "list"); err == nil {
		t.Fatal("want error")
	}
}

func TestResolveExecutorFallback(t *testing.T) {
	exec := &fakeExecutor{}
	s := NewSender(nil, &config.Config{}, exec, nil)
	if got := s.resolveExecutor("node-1"); got != DockerExecutor(exec) {
		t.Fatal("nil pool must resolve the default executor")
	}
}

func TestSendCommandRedactsRCONPasswordInError(t *testing.T) {
	// If rcon command fails with password leaked in error string, verify it is redacted
	rconPassword := "superSecretPass123"
	errMsg := fmt.Sprintf("dial failed to rcon://10.0.0.1:25575 with pass %s: connection refused", rconPassword)
	err := errors.New(errMsg)
	if rconPassword != "" && strings.Contains(err.Error(), rconPassword) {
		err = errors.New(strings.ReplaceAll(err.Error(), rconPassword, "********"))
	}
	if strings.Contains(err.Error(), rconPassword) {
		t.Errorf("expected password to be redacted from error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "********") {
		t.Errorf("expected placeholder in error, got: %v", err)
	}
}

func TestSendCommand_NilExecutorHandledGracefully(t *testing.T) {
	store := testStore(t)
	makeServer(t, store, "s-nil-exec", "c-nil-exec")
	s := NewSender(store, &config.Config{}, nil, nil)
	_, err := s.SendCommand(context.Background(), "s-nil-exec", "list")
	if err == nil {
		t.Fatal("expected error when executor is nil, got nil")
	}
	if !strings.Contains(err.Error(), "no docker executor available") {
		t.Errorf("expected 'no docker executor available', got: %v", err)
	}
}

func TestSendCommand_NilConfigHandledGracefully(t *testing.T) {
	store := testStore(t)
	makeServer(t, store, "s-nil-cfg", "c-nil-cfg")
	exec := &fakeExecutor{output: "ok"}
	s := NewSender(store, nil, exec, nil)
	out, err := s.SendCommand(context.Background(), "s-nil-cfg", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected 'ok', got %q", out)
	}
}

func TestSendCommandRemoteNodeExecutesDirectly(t *testing.T) {
	store := testStore(t)
	// Create a remote node
	node := &storage.Node{
		ID:      "remote-node-1",
		Name:    "remote-node",
		Host:    "tcp://192.168.0.200:2375",
		IsLocal: false,
		Status:  storage.NodeStatusOnline,
	}
	if err := store.CreateNode(context.Background(), node); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	srv := &storage.Server{
		ID:          "s-remote",
		Name:        "remote-server",
		ContainerID: "c-remote",
		NodeID:      "remote-node-1",
		Status:      storage.StatusRunning,
	}
	if err := store.CreateServer(context.Background(), srv); err != nil {
		t.Fatalf("SaveServer: %v", err)
	}

	exec := &fakeExecutor{output: "remote-exec-success"}
	s := testSender(store, exec, nil)

	out, err := s.SendCommand(context.Background(), "s-remote", "say hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "remote-exec-success" {
		t.Fatalf("expected 'remote-exec-success', got %q", out)
	}
	if exec.calls != 1 || exec.lastID != "c-remote" {
		t.Fatalf("expected 1 call to container c-remote, got %d calls to %q", exec.calls, exec.lastID)
	}
}
