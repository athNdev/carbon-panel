// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package rcon

import (
	"context"
	"testing"
	"time"
)

func TestSendCommandCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Unroutable host: dial blocks, so the already-cancelled context wins.
	_, err := SendCommand(ctx, "10.255.255.1", 25575, "pw", "list")
	if err != context.Canceled {
		t.Fatalf("err=%v want context.Canceled", err)
	}
}

func TestSendCommandRefusedHostErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Nothing listens on port 1: dial fails fast with a wrapped error.
	if _, err := SendCommand(ctx, "127.0.0.1", 1, "pw", "list"); err == nil {
		t.Fatal("want error for connection-refused host")
	}
}
