package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/moby/moby/client"
)

func TestRecreateContainerRetainsOriginalIDWhenStopFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("Api-Version", "1.45")
			w.Header().Set("Ostype", "linux")
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/containers/original/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"original","State":{"Status":"running","Running":true}}`))
		case strings.HasSuffix(r.URL.Path, "/containers/original/stop"):
			http.Error(w, `{"message":"stop failed"}`, http.StatusInternalServerError)
		case strings.HasSuffix(r.URL.Path, "/containers/original/kill"):
			http.Error(w, `{"message":"kill failed"}`, http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	sdk, err := NewAPIClient(client.WithHost(fakeDockerHost(t, srv)))
	if err != nil {
		t.Fatalf("NewAPIClient: %v", err)
	}
	defer func() { _ = sdk.Close() }()

	dockerClient := &Client{docker: sdk, log: logger.New()}
	result, err := dockerClient.RecreateContainer(context.Background(), "original", &models.Server{}, &models.ServerConfig{})
	if err == nil {
		t.Fatal("RecreateContainer() error = nil, want stop failure")
	}
	if result == nil || result.NewContainerID != "original" {
		t.Fatalf("RecreateContainer() result = %+v, want original ID retained", result)
	}
	if !result.WasRunning {
		t.Fatal("RecreateContainer() WasRunning = false, want true")
	}
}

func TestRecreateContainerUnpausesFrozenContainerBeforeRemoval(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	stopCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()

		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("Api-Version", "1.45")
			w.Header().Set("Ostype", "linux")
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/containers/paused/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"paused","State":{"Status":"paused","Running":true,"Paused":true}}`))
		case strings.HasSuffix(r.URL.Path, "/containers/paused/stop"):
			stopCalls++
			if stopCalls == 1 {
				http.Error(w, `{"message":"container is paused"}`, http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/containers/paused/unpause"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/containers/paused"):
			// Fail removal to stop the test before container creation. The original
			// identity must still be returned after it has been safely stopped.
			http.Error(w, `{"message":"remove temporarily unavailable"}`, http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	sdk, err := NewAPIClient(client.WithHost(fakeDockerHost(t, srv)))
	if err != nil {
		t.Fatalf("NewAPIClient: %v", err)
	}
	defer func() { _ = sdk.Close() }()

	dockerClient := &Client{docker: sdk, log: logger.New()}
	result, err := dockerClient.RecreateContainer(context.Background(), "paused", &models.Server{}, &models.ServerConfig{})
	if err == nil {
		t.Fatal("RecreateContainer() error = nil, want removal failure")
	}
	if result == nil || result.NewContainerID != "paused" {
		t.Fatalf("RecreateContainer() result = %+v, want original ID retained", result)
	}
	if result.WasRunning {
		t.Fatal("RecreateContainer() WasRunning = true for a previously frozen container")
	}
	if stopCalls != 2 {
		t.Fatalf("stop calls = %d, want initial failure and retry after unpause", stopCalls)
	}

	mu.Lock()
	defer mu.Unlock()
	unpauseAt, finalStopAt, removeAt := -1, -1, -1
	for i, call := range calls {
		if strings.HasSuffix(call, "/containers/paused/unpause") {
			unpauseAt = i
		}
		if strings.HasSuffix(call, "/containers/paused/stop") && i > 0 {
			finalStopAt = i
		}
		if strings.HasPrefix(call, "DELETE ") && strings.HasSuffix(call, "/containers/paused") {
			removeAt = i
		}
	}
	if unpauseAt < 0 || finalStopAt < unpauseAt || removeAt < finalStopAt {
		t.Fatalf("Docker call order = %v, want unpause, retry stop, then remove", calls)
	}
}
