// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package proxy

import "testing"

func TestDockerHostAddress(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"tcp://192.168.1.100:2376", "192.168.1.100"},
		{"tcp://[2001:db8::1]:2376", "2001:db8::1"},
		{"2001:db8::10", "2001:db8::10"},
		{"192.168.5.10", "192.168.5.10"},
	}
	for _, tc := range cases {
		if got := dockerHostAddress(tc.in); got != tc.want {
			t.Errorf("dockerHostAddress(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
