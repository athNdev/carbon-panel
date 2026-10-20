// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package proxy

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHandshakeRateLimitSkipsSecondWake(t *testing.T) {
	var wakes atomic.Int32
	proxy := NewMinecraftProxy(&Config{
		ListenAddr: "127.0.0.1:0",
		WakeHandler: func(context.Context, string) error {
			wakes.Add(1)
			return nil
		},
		LoginRateLimit: time.Hour,
	})
	if err := proxy.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proxy.Stop() }()

	proxy.AddRoute("srv", "sleep.test.local", "127.0.0.1", 1)
	proxy.SetRouteHibernated("sleep.test.local", true)

	handshake := &HandshakePacket{
		ProtocolVersion: 763,
		ServerAddress:   "sleep.test.local",
		ServerPort:      25565,
		NextState:       2,
	}
	dial := func() {
		t.Helper()
		conn, err := net.DialTimeout("tcp", proxy.listener.Addr().String(), 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		if err := WriteHandshakePacket(conn, handshake); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	dial()
	require.Eventually(t, func() bool { return wakes.Load() == 1 }, 2*time.Second, 10*time.Millisecond)
	dial()
	time.Sleep(150 * time.Millisecond)
	if got := wakes.Load(); got != 1 {
		t.Fatalf("wake calls = %d, want 1", got)
	}
}
