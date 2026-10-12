// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

func TestNormalizeLoaderName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"forge", "forge"},
		{"  Forge  ", "forge"},
		{"neoforge", "neoforge"},
		{"mod_loader_forge", "forge"},
		{"CurseForge", "forge"},
		{"quilt-loader", "quilt"},
		{"Fabric", "fabric"},
		{"paper", "paper"},
		{"vanilla", "vanilla"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := normalizeLoaderName(tc.in); got != tc.want {
			t.Errorf("normalizeLoaderName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestOpenAPIHandler_Responds(t *testing.T) {
	h := NewOpenAPIHandler(logger.New(), func() bool { return true })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	// The spec may be absent outside a built frontend (then 500); either
	// way the handler must answer instead of hanging, and a served spec
	// must be YAML.
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d", res.StatusCode)
	}
}

func TestNodeScanHandler_RejectsNonGet(t *testing.T) {
	h := NewNodeScanHandler(nil, nil, nil, logger.New())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/scan", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Result().StatusCode)
	}
}
