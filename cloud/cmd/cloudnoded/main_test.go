package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"connectrpc.com/connect"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
)

func TestHealthcheckCommand(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"healthcheck", ts.URL + "/healthz"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected 0, got %d, stderr: %s", code, stderr.String())
	}
	if stdout.String() != "ok\n" {
		t.Fatalf("expected 'ok\\n', got %q", stdout.String())
	}

	// Failure case
	stdout.Reset()
	stderr.Reset()
	badCode := run([]string{"healthcheck", "http://127.0.0.1:1/nonexistent"}, &stdout, &stderr)
	if badCode != 1 {
		t.Fatalf("expected exit 1 on connection failure, got %d", badCode)
	}
}

func TestIdentityPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	idPath := filepath.Join(tmpDir, "identity.json")

	orig := &PersistedIdentity{
		NodeID:                   "node_12345",
		OrgID:                    "org_test",
		ControlPlaneEndpoint:     "http://localhost:8080",
		HeartbeatIntervalSeconds: 15,
	}

	if err := saveIdentity(idPath, orig); err != nil {
		t.Fatalf("saveIdentity: %v", err)
	}

	loaded, err := loadIdentity(idPath)
	if err != nil {
		t.Fatalf("loadIdentity: %v", err)
	}
	if loaded.NodeID != orig.NodeID || loaded.OrgID != orig.OrgID {
		t.Fatalf("identity mismatch: %+v != %+v", loaded, orig)
	}
}

type fakeAgentHandler struct {
	cloudv1connect.UnimplementedAgentServiceHandler
	joined     bool
	heartbeats int
}

func (f *fakeAgentHandler) JoinNode(ctx context.Context, req *connect.Request[v1.JoinNodeRequest]) (*connect.Response[v1.JoinNodeResponse], error) {
	f.joined = true
	return connect.NewResponse(&v1.JoinNodeResponse{
		Identity: &v1.NodeIdentity{
			NodeId:                   "node_test_01",
			OrgId:                    "org_test_01",
			ControlPlaneEndpoint:     "http://127.0.0.1",
			HeartbeatIntervalSeconds: 1,
		},
		Node: &v1.Node{
			Id:   "node_test_01",
			Name: "test-node",
		},
	}), nil
}

func (f *fakeAgentHandler) Connect(ctx context.Context, stream *connect.BidiStream[v1.AgentMessage, v1.ControlMessage]) error {
	for {
		msg, err := stream.Receive()
		if err != nil {
			return err
		}
		switch payload := msg.Payload.(type) {
		case *v1.AgentMessage_Hello:
			_ = stream.Send(&v1.ControlMessage{
				Payload: &v1.ControlMessage_Welcome{
					Welcome: &v1.ControlWelcome{
						HeartbeatIntervalSeconds: 1,
					},
				},
			})
		case *v1.AgentMessage_Heartbeat:
			if payload.Heartbeat.NodeId == "node_test_01" {
				f.heartbeats++
			}
		}
	}
}

func TestAgentLoop_JoinAndConnect(t *testing.T) {
	fake := &fakeAgentHandler{}
	mux := http.NewServeMux()
	path, h := cloudv1connect.NewAgentServiceHandler(fake)
	mux.Handle(path, h)

	ts := httptest.NewServer(h2c.NewHandler(mux, &http2.Server{}))
	defer ts.Close()

	tmpDir := t.TempDir()
	idPath := filepath.Join(tmpDir, "identity.json")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	state := &AgentState{}
	logger := slogNew()

	go runAgentLoop(ctx, logger, state, ts.URL, "valid-join-token", idPath)

	// Wait for node to join and connect
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state.mu.RLock()
		connected := state.connected
		joined := state.identity != nil
		state.mu.RUnlock()
		if joined && connected && fake.heartbeats > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	state.mu.RLock()
	defer state.mu.RUnlock()
	if state.identity == nil || state.identity.NodeID != "node_test_01" {
		t.Fatalf("expected node to be joined with ID node_test_01, got %+v", state.identity)
	}
	if !state.connected {
		t.Fatalf("expected agent to be connected")
	}
	if fake.heartbeats == 0 {
		t.Fatalf("expected at least 1 heartbeat received")
	}

	// Check file was persisted
	if _, err := os.Stat(idPath); err != nil {
		t.Fatalf("expected identity file to exist: %v", err)
	}
}

func slogNew() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}
