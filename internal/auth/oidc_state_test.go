// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package auth

import "testing"

func TestSameOIDCState(t *testing.T) {
	tests := []struct {
		name          string
		cookieState   string
		callbackState string
		want          bool
	}{
		{name: "matching", cookieState: "state-token", callbackState: "state-token", want: true},
		{name: "different value", cookieState: "state-token", callbackState: "state-t0ken", want: false},
		{name: "different length", cookieState: "state-token", callbackState: "state", want: false},
		{name: "empty values do not authenticate", cookieState: "", callbackState: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameOIDCState(tt.cookieState, tt.callbackState); got != tt.want {
				t.Fatalf("sameOIDCState(%q, %q) = %v, want %v", tt.cookieState, tt.callbackState, got, tt.want)
			}
		})
	}
}
