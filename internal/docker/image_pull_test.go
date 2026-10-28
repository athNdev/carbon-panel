// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

func newImagePullTestClient(t *testing.T, handler http.Handler) (*Client, func()) {
	t.Helper()

	srv := httptest.NewServer(handler)
	sdk, err := NewAPIClient(client.WithHost(fakeDockerHost(t, srv)))
	if err != nil {
		srv.Close()
		t.Fatalf("NewAPIClient: %v", err)
	}
	return &Client{docker: sdk}, func() {
		_ = sdk.Close()
		srv.Close()
	}
}

func TestPullImageReturnsEmbeddedDockerStreamError(t *testing.T) {
	var pullRequests int
	httpServer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_ping":
			w.Header().Set("Api-Version", "1.45")
			w.Header().Set("Ostype", "linux")
			w.WriteHeader(http.StatusOK)
		case "/v1.45/images/create":
			pullRequests++
			if r.Method != http.MethodPost {
				t.Errorf("pull method = %s, want POST", r.Method)
			}
			if got := r.URL.Query().Get("fromImage"); got != "docker.io/library/example" {
				t.Errorf("fromImage = %q, want docker.io/library/example", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"errorDetail":{"message":"pull access denied for example","code":403},"error":"pull access denied for example"}` + "\n"))
		default:
			http.NotFound(w, r)
		}
	})
	c, cleanup := newImagePullTestClient(t, httpServer)
	defer cleanup()

	err := c.pullImage(context.Background(), "example")
	if err == nil {
		t.Fatal("pullImage returned nil for a Docker error frame in an HTTP 200 response")
	}
	if !strings.Contains(err.Error(), "pull access denied for example") {
		t.Fatalf("pullImage error %q does not include embedded Docker error", err)
	}
	if pullRequests != 1 {
		t.Fatalf("image pull requests = %d, want 1", pullRequests)
	}
}

func TestPullImageAcceptsCompletedProgressStream(t *testing.T) {
	httpServer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_ping":
			w.Header().Set("Api-Version", "1.45")
			w.Header().Set("Ostype", "linux")
			w.WriteHeader(http.StatusOK)
		case "/v1.45/images/create":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"Pull complete","id":"layer-id"}` + "\n"))
		default:
			http.NotFound(w, r)
		}
	})
	c, cleanup := newImagePullTestClient(t, httpServer)
	defer cleanup()

	if err := c.pullImage(context.Background(), "example"); err != nil {
		t.Fatalf("pullImage returned error for successful progress stream: %v", err)
	}
}

func TestEnsureImageSkipsPullWhenTheDaemonAlreadyHasIt(t *testing.T) {
	var pulls atomic.Int32
	c, cleanup := newImagePullTestClient(t, imageFixture(true, &pulls, nil))
	t.Cleanup(cleanup)

	id, err := c.ensureImage(context.Background(), "example:latest")
	if err != nil {
		t.Fatal(err)
	}
	if id != "sha256:fixture" {
		t.Fatalf("image id = %q, want sha256:fixture", id)
	}
	if pulls.Load() != 0 {
		t.Fatalf("pulls = %d, want 0", pulls.Load())
	}
}

func TestEnsureImageFailsWhenThePullDoesNotLeaveAnImage(t *testing.T) {
	var pulls atomic.Int32
	c, cleanup := newImagePullTestClient(t, imageFixture(false, &pulls, nil))
	t.Cleanup(cleanup)

	_, err := c.ensureImage(context.Background(), "example:latest")
	if err == nil || !strings.Contains(err.Error(), "not present after pull") {
		t.Fatalf("ensureImage() error = %v, want image missing after pull", err)
	}
	if pulls.Load() != 1 {
		t.Fatalf("pulls = %d, want 1", pulls.Load())
	}
}

func TestEnsureImageCoalescesConcurrentPulls(t *testing.T) {
	var pulls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	c, cleanup := newImagePullTestClient(t, imageFixture(false, &pulls, func() {
		once.Do(func() { close(started) })
		<-release
	}))
	t.Cleanup(cleanup)

	const ref = "example:latest"
	errCh := make(chan error, 2)
	go func() {
		_, err := c.ensureImage(context.Background(), ref)
		errCh <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("pull did not start")
	case err := <-errCh:
		t.Fatalf("first pull finished before the gate: %v", err)
	}
	go func() {
		_, err := c.ensureImage(context.Background(), ref)
		errCh <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for c.pulls.waiterCount(ref) < 1 {
		if time.Now().After(deadline) {
			t.Fatal("second pull did not join the in-flight request")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-errCh; err == nil || !strings.Contains(err.Error(), "not present after pull") {
			t.Fatalf("ensureImage() error = %v, want a shared pull that leaves no image", err)
		}
	}
	if pulls.Load() != 1 {
		t.Fatalf("pulls = %d, want 1", pulls.Load())
	}
}

func imageFixture(present bool, pulls *atomic.Int32, duringPull func()) http.Handler {
	var have atomic.Bool
	have.Store(present)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("Api-Version", "1.45")
			w.Header().Set("Ostype", "linux")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			if !have.Load() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"sha256:fixture"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/images/create"):
			pulls.Add(1)
			if duringPull != nil {
				duringPull()
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"Pull complete","id":"layer-id"}` + "\n"))
		default:
			http.NotFound(w, r)
		}
	})
}
