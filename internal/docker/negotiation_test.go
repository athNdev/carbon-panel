package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	models "github.com/athNdev/carbon-panel/internal/db"
	"github.com/docker/docker/client"
)

// fakeDockerHost converts an httptest server URL into the tcp:// docker host
// form the SDK accepts.
func fakeDockerHost(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	host := strings.TrimPrefix(srv.URL, "http://")
	if host == "" || host == srv.URL {
		t.Fatalf("unexpected httptest URL %q", srv.URL)
	}
	return "tcp://" + host
}

// TestNewAPIClientNegotiatesVersionEagerly pins the behaviour the panel relies
// on: API-version negotiation happens before the client is handed out (so the
// SDK's non-cancellable negotiate lock is never held across a network call),
// and the negotiated version is what subsequent requests use.
//
// If negotiation ever goes back to the SDK's lazy path, ClientVersion is still
// the SDK default right after NewAPIClient returns and this fails.
func TestNewAPIClientNegotiatesVersionEagerly(t *testing.T) {
	var mu sync.Mutex
	var paths []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()

		switch r.URL.Path {
		case "/_ping":
			w.Header().Set("Api-Version", "1.45")
			w.Header().Set("Ostype", "linux")
			w.WriteHeader(http.StatusOK)
		case "/v1.45/containers/abc/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"abc","State":{"Status":"running","Running":true,"ExitCode":0}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	sdk, err := NewAPIClient(client.WithHost(fakeDockerHost(t, srv)))
	if err != nil {
		t.Fatalf("NewAPIClient: %v", err)
	}
	defer func() { _ = sdk.Close() }()

	if got := sdk.ClientVersion(); got != "1.45" {
		t.Fatalf("ClientVersion() = %q immediately after NewAPIClient, want 1.45 (negotiation is lazy again)", got)
	}

	c := &Client{docker: sdk}
	status, err := c.GetContainerStatus(context.Background(), "abc")
	if err != nil {
		t.Fatalf("GetContainerStatus: %v", err)
	}
	if status != models.StatusRunning {
		t.Fatalf("status = %v, want running", status)
	}

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, p := range paths {
		if p == "/v1.45/containers/abc/json" {
			found = true
		}
	}
	if !found {
		t.Fatalf("inspect did not use the negotiated version; paths: %v", paths)
	}
}

// TestConcurrentCallersUseTheirOwnBudgets is the regression test for the
// "socket hang up" failure mode: the SDK's lazy negotiation pings while holding
// a non-cancellable mutex, so one unresponsive daemon used to block every other
// caller of that client regardless of its deadline. Each call must instead only
// wait for its own budget.
func TestConcurrentCallersUseTheirOwnBudgets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("Api-Version", "1.51")
			w.Header().Set("Ostype", "linux")
			w.WriteHeader(http.StatusOK)
			return
		}
		// API calls hang until the caller gives up.
		<-r.Context().Done()
	}))
	defer srv.Close()

	sdk, err := NewAPIClient(client.WithHost(fakeDockerHost(t, srv)))
	if err != nil {
		t.Fatalf("NewAPIClient: %v", err)
	}
	defer func() { _ = sdk.Close() }()

	c := &Client{docker: sdk}

	// First caller occupies the (unresponsive) daemon.
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = c.GetContainerStatus(ctx, "first")
	}()
	time.Sleep(150 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	start := time.Now()
	if _, err := c.GetContainerStatus(ctx, "second"); err == nil {
		t.Fatal("expected an error from the unresponsive daemon")
	}
	if elapsed := time.Since(start); elapsed > 1800*time.Millisecond {
		t.Fatalf("second caller blocked %v behind the first (want its own ~1s budget)", elapsed)
	}

	<-firstDone
}
