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
	"github.com/tidwall/gjson"
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

func TestResolveClaimRoles(t *testing.T) {
	h := testOIDCHandler("groups", nil, false)

	got := h.resolveClaimRoles(map[string]any{"groups": []any{"admins", "devs", 42}})
	if len(got) != 2 || got[0] != "admins" || got[1] != "devs" {
		t.Errorf("array claim = %v", got)
	}

	got = h.resolveClaimRoles(map[string]any{"groups": `["a","b"]`})
	if len(got) != 2 || got[0] != "a" {
		t.Errorf("json-string claim = %v", got)
	}

	got = h.resolveClaimRoles(map[string]any{"groups": "solo"})
	if len(got) != 1 || got[0] != "solo" {
		t.Errorf("string claim = %v", got)
	}

	if got := h.resolveClaimRoles(map[string]any{}); len(got) != 0 {
		t.Errorf("missing claim = %v", got)
	}
	if got := testOIDCHandler("", nil, false).resolveClaimRoles(map[string]any{"groups": []any{"x"}}); len(got) != 0 {
		t.Errorf("empty RoleClaim = %v", got)
	}

	// Explicit mapping with case-insensitive match.
	mapped := testOIDCHandler("groups", map[string]string{"Admins": "admin", "Devs": "user"}, false)
	got = mapped.resolveClaimRoles(map[string]any{"groups": []any{"ADMINS", "unknown"}})
	if len(got) != 1 || got[0] != "admin" {
		t.Errorf("mapped = %v", got)
	}

	// RejectUnmapped drops everything when a mapping exists.
	strict := testOIDCHandler("groups", map[string]string{"admins": "admin"}, true)
	if got := strict.resolveClaimRoles(map[string]any{"groups": []any{"other"}}); len(got) != 0 {
		t.Errorf("strict unmapped = %v", got)
	}
}

func TestCheckRequiredClaim(t *testing.T) {
	h := testOIDCHandler("", nil, false)
	h.config.RequiredClaim = "hd"
	h.config.RequiredValues = []string{"example.com"}

	if !h.checkRequiredClaim(map[string]any{"hd": "example.com"}) {
		t.Error("matching claim rejected")
	}
	if h.checkRequiredClaim(map[string]any{"hd": "evil.com"}) {
		t.Error("non-matching claim accepted")
	}
	if h.checkRequiredClaim(map[string]any{}) {
		t.Error("missing claim accepted")
	}

	h.config.RequiredClaim = ""
	if h.checkRequiredClaim(map[string]any{}) {
		t.Error("empty RequiredClaim accepted everything (fail-closed expected)")
	}
}

func TestGjsonToAny(t *testing.T) {
	if got := gjsonToAny(gjson.Parse(`[1,"a",true]`)); true {
		arr, ok := got.([]any)
		if !ok || len(arr) != 3 {
			t.Errorf("array = %#v", got)
		}
	}
	if got := gjsonToAny(gjson.Parse(`{"k":"v"}`)); true {
		obj, ok := got.(map[string]any)
		if !ok || obj["k"] != "v" {
			t.Errorf("object = %#v", got)
		}
	}
	if got := gjsonToAny(gjson.Parse(`42`)); got != float64(42) {
		t.Errorf("number = %#v", got)
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
			w.Write([]byte(`{"groups":["a"],"nested":{"x":1}}`))
		case "/invalid":
			w.Write([]byte(`not json`))
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
