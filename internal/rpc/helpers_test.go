// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package rpc

import (
	"net/http/httptest"
	"testing"
)

func TestIsPollingProcedure(t *testing.T) {
	s := &Server{}
	for _, p := range pollingProcedures {
		if !s.isPollingProcedure(p) {
			t.Errorf("polling procedure %q not recognized", p)
		}
	}
	if s.isPollingProcedure("/carbonpanel.v1.ServerService/DeleteServer") {
		t.Error("mutating procedure classified as polling")
	}
	if s.isPollingProcedure("") {
		t.Error("empty procedure classified as polling")
	}
}

func TestSetFrontendCacheHeaders(t *testing.T) {
	immutable := httptest.NewRecorder()
	setFrontendCacheHeaders(immutable, "/_app/immutable/chunk.abc.js")
	if got := immutable.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("immutable = %q", got)
	}

	html := httptest.NewRecorder()
	setFrontendCacheHeaders(html, "/index.html")
	if got := html.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("html = %q", got)
	}

	plain := httptest.NewRecorder()
	setFrontendCacheHeaders(plain, "/favicon.png")
	if got := plain.Header().Get("Cache-Control"); got != "" {
		t.Errorf("plain asset = %q, want unset", got)
	}
}

func TestIsConnectPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/carbonpanel.v1.ServerService/ListServers", true},
		{"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo", true},
		{"/connect.protocol", true},
		{"/", false},
		{"/servers", false},
		{"/api/v1/openapi.yaml", false},
		{"", false},
		{"/carbonpanel.v1.", false},
	}
	for _, tc := range cases {
		if got := isConnectPath(tc.path); got != tc.want {
			t.Errorf("isConnectPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
