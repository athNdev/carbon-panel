// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

func testOIDCHandler(roleClaim string, mapping map[string]string, rejectUnmapped bool) *OIDCHandler {
	return &OIDCHandler{
		config: &config.OIDCConfig{
			RoleClaim:      roleClaim,
			RoleMapping:    mapping,
			RejectUnmapped: rejectUnmapped,
		},
		log: logger.New(),
	}
}

func TestFetchExtraClaims(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/claims":
			_, _ = w.Write([]byte(`{"groups":["a"],"nested":{"x":1}}`))
		case "/invalid":
			_, _ = w.Write([]byte(`not json`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	newHandler := func(path, key, name string) *OIDCHandler {
		h := testOIDCHandler("", nil, false)
		h.config.ExtraClaimsURL = srv.URL + path
		h.config.ExtraClaimsKey = key
		h.config.ExtraClaimsName = name
		h.httpClient = srv.Client()
		return h
	}

	got, err := newHandler("/claims", "groups", "teams").fetchExtraClaims(context.Background(), "tok123")
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	teams, ok := got["teams"].([]any)
	if !ok || len(teams) != 1 || teams[0] != "a" {
		t.Errorf("teams = %#v", got)
	}

	got, err = newHandler("/claims", "", "").fetchExtraClaims(context.Background(), "tok123")
	if err != nil {
		t.Fatalf("whole-body fetch failed: %v", err)
	}
	if _, ok := got["extra"]; !ok {
		t.Errorf("whole-body = %#v", got)
	}

	if _, err := newHandler("/claims", "missing.deep", "").fetchExtraClaims(context.Background(), "tok123"); err == nil {
		t.Error("missing key accepted")
	}
	if _, err := newHandler("/invalid", "", "").fetchExtraClaims(context.Background(), "tok123"); err == nil {
		t.Error("invalid JSON accepted")
	}
	if _, err := newHandler("/nope", "", "").fetchExtraClaims(context.Background(), "tok123"); err == nil {
		t.Error("404 accepted")
	}
	if _, err := newHandler("/claims", "", "").fetchExtraClaims(context.Background(), "wrong"); err == nil {
		t.Error("bad token accepted")
	}
}
