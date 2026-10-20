// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionTokenFromRequestIgnoresQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ws?token=session.jwt.here", nil)
	if got := SessionTokenFromRequest(req); got != "" {
		t.Fatalf("query token = %q, want empty", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/ws?token=ignored", nil)
	req.Header.Set("Authorization", "Bearer header-token")
	if got := SessionTokenFromRequest(req); got != "header-token" {
		t.Fatalf("header token = %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/download?token=ignored", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "cookie-token"})
	if got := SessionTokenFromRequest(req); got != "cookie-token" {
		t.Fatalf("cookie token = %q", got)
	}
}
