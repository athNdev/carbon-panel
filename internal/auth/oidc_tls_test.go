// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package auth

import "testing"

func TestOIDCSkipTLSVerifyRequiresEscape(t *testing.T) {
	t.Setenv("CARBON_ALLOW_INSECURE_OIDC_TLS", "")
	if oidcInsecureSkipVerify(true) {
		t.Fatal("skip_tls_verify alone must not disable verification")
	}
	t.Setenv("CARBON_ALLOW_INSECURE_OIDC_TLS", "1")
	if !oidcInsecureSkipVerify(true) {
		t.Fatal("process escape plus the config flag may skip verification")
	}
	if oidcInsecureSkipVerify(false) {
		t.Fatal("process escape without the config flag must keep verification")
	}
}
