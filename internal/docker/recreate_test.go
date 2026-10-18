package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/moby/moby/client"
)

func TestRecreateContainerRetainsOriginalIDWhenStopFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
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

	sdk, err := NewAPIClient(client.WithHost(fakeDockerHost(t, srv)), client.WithVersion("1.45"))
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
