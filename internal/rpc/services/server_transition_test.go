// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"testing"

	"connectrpc.com/connect"
)

// TestBeginServerLifecycleTransitionErrors guards the regression where a
// missing server id produced the misleading "server transition already in
// progress" (TryBeginServerTransition("") fails), wasting integrators' time.
func TestBeginServerLifecycleTransitionErrors(t *testing.T) {
	for _, id := range []string{"", "   "} {
		_, err := beginServerLifecycleTransition(id)
		if err == nil {
			t.Fatalf("id %q: want error", id)
		}
		if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
			t.Fatalf("id %q: code = %v, want invalid_argument", id, got)
		}
	}

	transition, err := beginServerLifecycleTransition("srv-guard")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer transition.Release()

	_, err = beginServerLifecycleTransition("srv-guard")
	if err == nil {
		t.Fatal("contended claim: want error")
	}
	if got := connect.CodeOf(err); got != connect.CodeAborted {
		t.Fatalf("contended claim: code = %v, want aborted", got)
	}
}
