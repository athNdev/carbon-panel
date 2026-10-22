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
	"testing"

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
