// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package pin

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type rewriteTripper struct {
	base http.RoundTripper
	dest string
}

func (r rewriteTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	next := req.Clone(req.Context())
	next.URL.Scheme = "http"
	next.URL.Host = r.dest
	next.Host = r.dest
	return r.base.RoundTrip(next)
}

func TestRemoteAuthDefaultsToFloodgate(t *testing.T) {
	auth, err := RemoteAuthType("", "")
	if err != nil || auth != "floodgate" {
		t.Fatalf("auth=%q err=%v", auth, err)
	}
	if _, err := RemoteAuthType("offline", ""); err == nil {
		t.Fatal("offline auth must fail closed")
	}
	auth, err = RemoteAuthType("offline", "1")
	if err != nil || auth != "offline" {
		t.Fatalf("auth=%q err=%v", auth, err)
	}
	if strings.Contains(FloodgateURL, "/latest") {
		t.Fatal("floodgate URL must be pinned")
	}
}

func TestInstallFloodgateWritesJar(t *testing.T) {
	body := []byte("floodgate-bytes")
	sum := sha256.Sum256(body)
	good := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	client := &http.Client{Transport: rewriteTripper{base: http.DefaultTransport, dest: host}}
	dir := t.TempDir()
	savedURL, savedSHA := FloodgateURL, FloodgateSHA256
	FloodgateURL = "https://download.geysermc.org/floodgate"
	FloodgateSHA256 = good
	t.Cleanup(func() {
		FloodgateURL = savedURL
		FloodgateSHA256 = savedSHA
	})
	if err := InstallFloodgate(dir, client); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "floodgate-spigot.jar")); err != nil {
		t.Fatal(err)
	}
}

func TestFetchPinnedGeyser(t *testing.T) {
	if strings.Contains(URL, "/latest") {
		t.Fatal("production URL must not use latest")
	}

	body := []byte("geyser-bytes")
	sum := sha256.Sum256(body)
	good := hex.EncodeToString(sum[:])
	redirect := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if redirect {
			http.Redirect(w, r, "https://evil.example/geyser.jar", http.StatusFound)
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	client := &http.Client{Transport: rewriteTripper{base: http.DefaultTransport, dest: host}}
	dir := t.TempDir()
	dest := filepath.Join(dir, "Geyser.jar")
	raw := "https://download.geysermc.org/standalone"

	if err := Fetch(client, raw, dest, "deadbeef", 1024); err == nil {
		t.Fatal("checksum mismatch must fail closed")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("failed download left a jar behind")
	}

	if err := Fetch(client, raw, dest, good, 1024); err != nil {
		t.Fatal(err)
	}

	redirect = true
	if err := Fetch(client, raw, filepath.Join(dir, "other.jar"), good, 1024); err == nil || !strings.Contains(err.Error(), "refusing redirect") {
		t.Fatalf("redirect err = %v", err)
	}
}
