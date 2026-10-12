// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package utils

import (
	"testing"
)

func TestDeduplicateStrings(t *testing.T) {
	got := DeduplicateStrings([]string{"b", "a", "b", "c", "a"})
	if len(got) != 3 || got[0] != "b" || got[1] != "a" || got[2] != "c" {
		t.Errorf("dedup = %v, want order-preserving unique", got)
	}
	if got := DeduplicateStrings(nil); len(got) != 0 {
		t.Errorf("dedup nil = %v", got)
	}
}

func TestEnsureMemoryHeadroom(t *testing.T) {
	// Configured above the required floor: untouched.
	limitMB, limitBytes := EnsureMemoryHeadroom(2048, 4096)
	if limitMB != 4096 || limitBytes != 4096*1024*1024 {
		t.Errorf("got %dMB/%dB", limitMB, limitBytes)
	}
	// Below floor: raised to required (heap*Ratio + off-heap buffer).
	raisedMB, raisedBytes := EnsureMemoryHeadroom(2048, 100)
	if raisedMB <= 2048 || raisedBytes != int64(raisedMB)*1024*1024 {
		t.Errorf("raised = %dMB/%dB", raisedMB, raisedBytes)
	}
}
