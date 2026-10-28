// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"net/http"
	"strings"
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
)

const (
	testConfigID = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	testRepoPin  = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testOtherPin = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestSelectRepoDigestUsesTheRequestedRepository(t *testing.T) {
	digests := []string{
		"ghcr.io/example/mirror@" + testOtherPin,
		"docker.io/itzg/minecraft-server@" + testRepoPin,
	}
	got := selectRepoDigest("itzg/minecraft-server:java21", digests)
	if got != "itzg/minecraft-server@"+testRepoPin {
		t.Fatalf("digest = %q", got)
	}
	if got == testConfigID {
		t.Fatal("repo digest collapsed to the image id")
	}
	if selectRepoDigest("itzg/minecraft-server:java21", nil) != "" {
		t.Fatal("missing repo digests produced a digest")
	}
	pinned := "itzg/minecraft-server@" + testOtherPin
	if selectRepoDigest(pinned, digests) != "" {
		t.Fatal("a pin that is not in the daemon repo digests was filled from another tag")
	}
}

func TestServerImageIdentitySeparatesRepoDigestFromImageID(t *testing.T) {
	body := `{"Id":"` + testConfigID + `","RepoDigests":["docker.io/itzg/minecraft-server@` + testRepoPin + `","ghcr.io/example/mirror@` + testOtherPin + `"]}`
	c, cleanup := newImagePullTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_ping":
			w.Header().Set("Api-Version", "1.45")
			w.Header().Set("Ostype", "linux")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/info"):
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(cleanup)

	id, digest, err := c.ServerImageIdentity(context.Background(), &models.Server{DockerImage: "java21"})
	if err != nil {
		t.Fatal(err)
	}
	if id != testConfigID {
		t.Fatalf("image id = %q", id)
	}
	if digest != "itzg/minecraft-server@"+testRepoPin {
		t.Fatalf("repo digest = %q", digest)
	}
}
