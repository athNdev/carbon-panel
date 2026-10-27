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
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/moby/moby/client"
)

// recreateServerFixture builds a fake Docker daemon for recreate tests. The
// server has a valid port (preflight rejects port-less servers) and responds
// to the image, inspect, stop, rename, create and start endpoints.
type recreateFixture struct {
	mu           sync.Mutex
	calls        []string
	stopCalls    int
	renameCalls  []string
	createFails  bool
	startFails   bool
	stopFails    bool
	renameFails  bool
	pullFails    bool
	imagePresent bool
	inspectCode  int
}

func (f *recreateFixture) record(method, path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method+" "+path)
}

func (f *recreateFixture) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *recreateFixture) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.record(r.Method, r.URL.Path)
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("Api-Version", "1.45")
			w.Header().Set("Ostype", "linux")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			if f.imagePresent {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"Id":"sha256:fixture"}`))
				return
			}
			http.NotFound(w, r)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/images/create"):
			if f.pullFails {
				http.Error(w, `{"message":"registry unavailable"}`, http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/containers/original/json") || strings.HasSuffix(r.URL.Path, "/containers/paused/json"):
			id := "original"
			state := `{"Status":"running","Running":true}`
			if strings.Contains(r.URL.Path, "/paused/") {
				id = "paused"
				state = `{"Status":"paused","Running":true,"Paused":true}`
			}
			if f.inspectCode != 0 && f.inspectCode != http.StatusOK {
				http.Error(w, `{"message":"daemon temporarily unavailable"}`, f.inspectCode)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"` + id + `","Name":"/` + id + `","State":` + state + `}`))
		case strings.HasSuffix(r.URL.Path, "/containers/original/stop") || strings.HasSuffix(r.URL.Path, "/containers/paused/stop"):
			f.mu.Lock()
			f.stopCalls++
			calls := f.stopCalls
			stopFails := f.stopFails
			f.mu.Unlock()
			if stopFails {
				http.Error(w, `{"message":"stop failed"}`, http.StatusInternalServerError)
				return
			}
			if strings.Contains(r.URL.Path, "/paused/") && calls == 1 {
				http.Error(w, `{"message":"container is paused"}`, http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/containers/original/kill") || strings.HasSuffix(r.URL.Path, "/containers/paused/kill"):
			http.Error(w, `{"message":"kill failed"}`, http.StatusInternalServerError)
		case strings.HasSuffix(r.URL.Path, "/containers/paused/unpause"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rename"):
			name := r.URL.Query().Get("name")
			f.mu.Lock()
			f.renameCalls = append(f.renameCalls, name)
			fails := f.renameFails
			f.mu.Unlock()
			if fails {
				http.Error(w, `{"message":"rename temporarily unavailable"}`, http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			if f.createFails {
				http.Error(w, `{"message":"create failed"}`, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"replacement","Warnings":[]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/replacement/start"):
			if f.startFails {
				http.Error(w, `{"message":"start failed"}`, http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/original/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && (strings.HasSuffix(r.URL.Path, "/containers/replacement") || strings.HasSuffix(r.URL.Path, "/containers/original")):
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}
}

func newRecreateClient(t *testing.T, f *recreateFixture) *Client {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	sdk, err := NewAPIClient(client.WithHost(fakeDockerHost(t, srv)))
	if err != nil {
		t.Fatalf("NewAPIClient: %v", err)
	}
	t.Cleanup(func() { _ = sdk.Close() })
	return &Client{docker: sdk, log: logger.New()}
}

func testRecreateServer(t *testing.T) *models.Server {
	return &models.Server{ID: "srv-1", Port: 25565, DataPath: t.TempDir()}
}

func TestRecreateContainerRetainsOriginalIDWhenStopFails(t *testing.T) {
	f := &recreateFixture{imagePresent: true, stopFails: true}
	dockerClient := newRecreateClient(t, f)

	result, err := dockerClient.RecreateContainer(context.Background(), "original", testRecreateServer(t), &models.ServerConfig{})
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

// TestRecreateContainerUnpausesFrozenContainerBeforeReplacement pins the
// frozen-container handling: a paused container is unpaused and stopped
// before it is retained, its replacement stays stopped (paused intent), and
// a failure while retaining keeps the original identity.
func TestRecreateContainerUnpausesFrozenContainerBeforeReplacement(t *testing.T) {
	f := &recreateFixture{imagePresent: true, renameFails: true}
	dockerClient := newRecreateClient(t, f)

	result, err := dockerClient.RecreateContainer(context.Background(), "paused", testRecreateServer(t), &models.ServerConfig{})
	if err == nil {
		t.Fatal("RecreateContainer() error = nil, want retention failure")
	}
	if result == nil || result.NewContainerID != "paused" {
		t.Fatalf("RecreateContainer() result = %+v, want original ID retained", result)
	}
	if result.WasRunning {
		t.Fatal("RecreateContainer() WasRunning = true for a previously frozen container")
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopCalls != 2 {
		t.Fatalf("stop calls = %d, want initial failure and retry after unpause", f.stopCalls)
	}
	unpauseAt, finalStopAt, renameAt := -1, -1, -1
	for i, call := range f.calls {
		if strings.HasSuffix(call, "/containers/paused/unpause") {
			unpauseAt = i
		}
		if strings.HasSuffix(call, "/containers/paused/stop") && i > 0 {
			finalStopAt = i
		}
		if strings.HasSuffix(call, "/rename") {
			renameAt = i
		}
	}
	if unpauseAt < 0 || finalStopAt < unpauseAt || renameAt < finalStopAt {
		t.Fatalf("Docker call order = %v, want unpause, retry stop, then retain", f.calls)
	}
}

func TestRecreateContainerDoesNotRemoveWhenInspectIsUnavailable(t *testing.T) {
	f := &recreateFixture{imagePresent: true, inspectCode: http.StatusServiceUnavailable}
	dockerClient := newRecreateClient(t, f)

	result, err := dockerClient.RecreateContainer(context.Background(), "original", testRecreateServer(t), &models.ServerConfig{})
	if err == nil || !strings.Contains(err.Error(), "inspect") {
		t.Fatalf("RecreateContainer() error = %v, want inspect failure", err)
	}
	if result == nil || result.NewContainerID != "original" {
		t.Fatalf("RecreateContainer() result = %+v, want original ID retained", result)
	}
	for _, call := range f.snapshot() {
		if strings.HasPrefix(call, "DELETE ") || strings.Contains(call, "/rename") || strings.Contains(call, "/stop") {
			t.Fatalf("destructive call %q sent while inspect outcome was unknown", call)
		}
	}
}

// TestRecreateContainerPullFailureLeavesOldContainerRunning pins the
// preflight: a failed image pull happens before the known-good container is
// touched at all — no stop, no rename, no remove.
func TestRecreateContainerPullFailureLeavesOldContainerRunning(t *testing.T) {
	f := &recreateFixture{imagePresent: false, pullFails: true}
	dockerClient := newRecreateClient(t, f)

	result, err := dockerClient.RecreateContainer(context.Background(), "original", testRecreateServer(t), &models.ServerConfig{})
	if err == nil {
		t.Fatal("RecreateContainer() error = nil, want pull failure")
	}
	if result == nil || result.NewContainerID != "original" {
		t.Fatalf("RecreateContainer() result = %+v, want original ID retained", result)
	}
	for _, call := range f.snapshot() {
		if strings.HasPrefix(call, "DELETE ") || strings.Contains(call, "/rename") || strings.Contains(call, "/stop") || strings.Contains(call, "/start") {
			t.Fatalf("old container was mutated despite failed preflight: %q", call)
		}
	}
}

// TestRecreateContainerCreateFailureRestoresOldContainer pins the rollback:
// when the replacement cannot be created, the retained old container gets
// its name and run state back and stays the tracked identity.
func TestRecreateContainerCreateFailureRestoresOldContainer(t *testing.T) {
	f := &recreateFixture{imagePresent: true, createFails: true}
	dockerClient := newRecreateClient(t, f)

	result, err := dockerClient.RecreateContainer(context.Background(), "original", testRecreateServer(t), &models.ServerConfig{})
	if err == nil {
		t.Fatal("RecreateContainer() error = nil, want create failure")
	}
	if result == nil || result.NewContainerID != "original" {
		t.Fatalf("RecreateContainer() result = %+v, want original ID retained", result)
	}
	if !result.WasRunning {
		t.Fatal("RecreateContainer() WasRunning = false, want true")
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.renameCalls) != 2 {
		t.Fatalf("rename calls = %v, want retain then restore", f.renameCalls)
	}
	if f.renameCalls[1] != "original" {
		t.Fatalf("second rename = %q, want original name restored", f.renameCalls[1])
	}
	restarted := false
	for _, call := range f.calls {
		if strings.HasPrefix(call, "DELETE ") {
			t.Fatalf("old container must not be deleted during rollback: %q", call)
		}
		if strings.HasSuffix(call, "/containers/original/start") {
			restarted = true
		}
	}
	if !restarted {
		t.Fatalf("restored container must be restarted to preserve run intent, calls = %v", f.calls)
	}
}

// TestRecreateContainerStartFailureRollsBackToOld pins rollback after a
// partial create: the unusable replacement is removed and the old identity is
// restored and restarted.
func TestRecreateContainerStartFailureRollsBackToOld(t *testing.T) {
	f := &recreateFixture{imagePresent: true, startFails: true}
	dockerClient := newRecreateClient(t, f)

	result, err := dockerClient.RecreateContainer(context.Background(), "original", testRecreateServer(t), &models.ServerConfig{})
	if err == nil {
		t.Fatal("RecreateContainer() error = nil, want start failure")
	}
	if result == nil || result.NewContainerID != "original" {
		t.Fatalf("RecreateContainer() result = %+v, want original ID retained", result)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.renameCalls) != 2 || f.renameCalls[1] != "original" {
		t.Fatalf("rename calls = %v, want retain then restore", f.renameCalls)
	}
	removedReplacement := false
	for _, call := range f.calls {
		if strings.HasSuffix(call, "/containers/original") && strings.HasPrefix(call, "DELETE ") {
			t.Fatalf("old container must not be deleted during rollback: %q", call)
		}
		if strings.HasSuffix(call, "/containers/replacement") && strings.HasPrefix(call, "DELETE ") {
			removedReplacement = true
		}
	}
	if !removedReplacement {
		t.Fatalf("unusable replacement must be removed, calls = %v", f.calls)
	}
}

// TestRecreateContainerSuccessRemovesRetainedOldAfterStart pins the cutover:
// the retained old container is only deleted after the replacement has been
// created and started.
func TestRecreateContainerSuccessRemovesRetainedOldAfterStart(t *testing.T) {
	f := &recreateFixture{imagePresent: true}
	dockerClient := newRecreateClient(t, f)

	result, err := dockerClient.RecreateContainer(context.Background(), "original", testRecreateServer(t), &models.ServerConfig{})
	if err != nil {
		t.Fatalf("RecreateContainer() error = %v", err)
	}
	if result == nil || result.NewContainerID != "replacement" {
		t.Fatalf("RecreateContainer() result = %+v, want replacement tracked", result)
	}
	if !result.WasRunning {
		t.Fatal("RecreateContainer() WasRunning = false, want true")
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	renameAt, createAt, startAt, removeAt := -1, -1, -1, -1
	for i, call := range f.calls {
		switch {
		case strings.HasSuffix(call, "/rename"):
			renameAt = i
		case strings.HasSuffix(call, "/containers/create"):
			createAt = i
		case strings.HasSuffix(call, "/containers/replacement/start"):
			startAt = i
		case strings.HasPrefix(call, "DELETE ") && strings.HasSuffix(call, "/containers/original"):
			removeAt = i
		}
	}
	if renameAt < 0 || createAt < renameAt {
		t.Fatalf("call order = %v, want retain before create", f.calls)
	}
	if startAt < createAt || removeAt < startAt {
		t.Fatalf("call order = %v, want start of replacement before removal of retained old", f.calls)
	}
}
