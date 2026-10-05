package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	storage "github.com/athNdev/carbon-panel/internal/db"
)

type fakeExecutor struct {
	output string
	err    error
	calls  int
}

func (f *fakeExecutor) ExecCommand(_ context.Context, _ string, _ string) (string, error) {
	f.calls++
	return f.output, f.err
}

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
