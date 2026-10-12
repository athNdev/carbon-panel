// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package db

import (
	"context"
	"testing"
)

func TestCreateSnapshot_NilRejected(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	if err := store.CreateSnapshot(context.Background(), nil); err == nil {
		t.Error("nil snapshot accepted")
	}
}

func TestSeedDefaultNode_NoOp(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	if err := store.SeedDefaultNode(); err != nil {
		t.Errorf("no-op seed failed: %v", err)
	}
}
