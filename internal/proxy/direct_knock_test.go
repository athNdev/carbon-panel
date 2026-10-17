// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package proxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// Helper to write Minecraft varint
func writeTestVarInt(buf *bytes.Buffer, val int) {
	uval := uint32(val)
	for {
		b := byte(uval & 0x7F)
		uval >>= 7
		if uval != 0 {
			b |= 0x80
		}
		buf.WriteByte(b)
		if uval == 0 {
			break
		}
	}
}

// Helper to build a Minecraft handshake packet
func buildTestHandshakePacket(protocolVersion int, hostname string, port uint16, nextState int) []byte {
	var body bytes.Buffer
	writeTestVarInt(&body, 0x00) // Packet ID: Handshake
	writeTestVarInt(&body, protocolVersion)

	// String: length prefix + utf8
	writeTestVarInt(&body, len(hostname))
	body.WriteString(hostname)

	// Port: 2 bytes big endian
	_ = binary.Write(&body, binary.BigEndian, port)

	// Next state
	writeTestVarInt(&body, nextState)

	var packet bytes.Buffer
	writeTestVarInt(&packet, body.Len())
	packet.Write(body.Bytes())
	return packet.Bytes()
}

// Helper to get a random free port on localhost
func getFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// Test 1: Direct knock listener starts and stops, binding the port
func TestDirectKnock_StartStop(t *testing.T) {
	port := getFreePort(t)
	log := logger.New()
	mgr := NewDirectKnockManager(nil, log)

	srv := &db.Server{
		ID:     "srv-start-stop",
		Name:   "Test Server",
		Port:   port,
		Status: db.StatusDeepSleep,
	}

	err := mgr.StartKnock(srv)
	if err != nil {
		t.Fatalf("expected StartKnock to succeed: %v", err)
	}

	if !mgr.HasKnock(srv.ID) {
		t.Fatalf("expected HasKnock to return true")
	}

	// Verify port is bound
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		t.Fatalf("failed to connect to knock listener on port %d: %v", port, err)
	}
	conn.Close()

	// Stop knock
	mgr.StopKnock(srv.ID)
	if mgr.HasKnock(srv.ID) {
		t.Fatalf("expected HasKnock to return false after StopKnock")
	}

	// Wait briefly for OS to release socket
	time.Sleep(50 * time.Millisecond)

	// Verify port is freed
	testLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("expected port %d to be freed after StopKnock: %v", port, err)
	}
	testLn.Close()
}

// Test 2: Direct knock responds with asleep MOTD on SLP ping (NextState=1)
func TestDirectKnock_SLPPing(t *testing.T) {
	port := getFreePort(t)
	log := logger.New()
	mgr := NewDirectKnockManager(nil, log)

	srv := &db.Server{
		ID:     "srv-slp-ping",
		Name:   "Test Server SLP",
		Port:   port,
		Status: db.StatusDeepSleep,
	}

	if err := mgr.StartKnock(srv); err != nil {
		t.Fatalf("StartKnock failed: %v", err)
	}
	defer mgr.StopAll()

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		t.Fatalf("failed to dial knock port: %v", err)
	}
	defer conn.Close()

	// 1. Send Handshake with NextState = 1 (Status)
	handshake := buildTestHandshakePacket(765, "127.0.0.1", uint16(port), 1)
	if _, err := conn.Write(handshake); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// 2. Send Status Request (Packet ID 0x00, empty body)
	var req bytes.Buffer
	writeTestVarInt(&req, 1) // length
	writeTestVarInt(&req, 0) // packet id 0
	if _, err := conn.Write(req.Bytes()); err != nil {
		t.Fatalf("failed to write status request: %v", err)
	}

	// 3. Read Status Response
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	respLen, err := ReadVarInt(conn)
	if err != nil {
		t.Fatalf("failed to read response length: %v", err)
	}
	respData := make([]byte, respLen)
	if _, err := io.ReadFull(conn, respData); err != nil {
		t.Fatalf("failed to read response data: %v", err)
	}

	// Packet ID should be 0x00
	reader := bytes.NewReader(respData)
	pktID, err := ReadVarInt(reader)
	if err != nil || pktID != 0 {
		t.Fatalf("expected packet id 0, got %d (err: %v)", pktID, err)
	}

	jsonLen, err := ReadVarInt(reader)
	if err != nil {
		t.Fatalf("failed to read json string length: %v", err)
	}
	jsonBytes := make([]byte, jsonLen)
	if _, err := io.ReadFull(reader, jsonBytes); err != nil {
		t.Fatalf("failed to read json payload: %v", err)
	}

	var payload struct {
		Description struct {
			Text string `json:"text"`
		} `json:"description"`
	}
	if err := json.Unmarshal(jsonBytes, &payload); err != nil {
		t.Fatalf("failed to unmarshal JSON status: %v", err)
	}

	if !strings.Contains(payload.Description.Text, "deep sleep") {
		t.Fatalf("expected MOTD to contain 'deep sleep', got: %s", payload.Description.Text)
	}
}

// Test 3: Direct knock releases port and sends disconnect packet on login intent (NextState=2)
func TestDirectKnock_LoginIntent_ReleasesPortAndDisconnects(t *testing.T) {
	port := getFreePort(t)
	log := logger.New()
	mgr := NewDirectKnockManager(nil, log)

	srv := &db.Server{
		ID:     "srv-login-intent",
		Name:   "Test Server Login",
		Port:   port,
		Status: db.StatusDeepSleep,
	}

	if err := mgr.StartKnock(srv); err != nil {
		t.Fatalf("StartKnock failed: %v", err)
	}
	defer mgr.StopAll()

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		t.Fatalf("failed to dial knock port: %v", err)
	}
	defer conn.Close()

	// Send Handshake with NextState = 2 (Login)
	handshake := buildTestHandshakePacket(765, "127.0.0.1", uint16(port), 2)
	if _, err := conn.Write(handshake); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// Read disconnect response
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	respLen, err := ReadVarInt(conn)
	if err != nil {
		t.Fatalf("failed to read disconnect packet length: %v", err)
	}
	respData := make([]byte, respLen)
	if _, err := io.ReadFull(conn, respData); err != nil {
		t.Fatalf("failed to read disconnect payload: %v", err)
	}

	// Packet ID 0x00 for Login Disconnect
	reader := bytes.NewReader(respData)
	pktID, err := ReadVarInt(reader)
	if err != nil || pktID != 0 {
		t.Fatalf("expected packet id 0, got %d", pktID)
	}
	msgLen, err := ReadVarInt(reader)
	if err != nil {
		t.Fatalf("failed to read disconnect message length: %v", err)
	}
	msgBytes := make([]byte, msgLen)
	if _, err := io.ReadFull(reader, msgBytes); err != nil {
		t.Fatalf("failed to read disconnect message: %v", err)
	}
	msg := string(msgBytes)
	if !strings.Contains(msg, "Carbon") || !strings.Contains(msg, "deep sleep") {
		t.Fatalf("expected disconnect message to mention Carbon and deep sleep, got: %s", msg)
	}

	// Verify listener is closed immediately so port is free
	time.Sleep(50 * time.Millisecond)
	if mgr.HasKnock(srv.ID) {
		t.Fatalf("expected knock listener to be stopped after login intent")
	}

	testLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("expected port to be free for Docker to bind: %v", err)
	}
	testLn.Close()
}

// Test 4: Direct knock triggers wake handler on login intent
func TestDirectKnock_LoginIntent_TriggersWakeHandler(t *testing.T) {
	port := getFreePort(t)
	log := logger.New()

	var woke atomic.Bool
	var wokeServerID string
	var mu sync.Mutex

	wakeHandler := func(ctx context.Context, serverID string) error {
		mu.Lock()
		wokeServerID = serverID
		mu.Unlock()
		woke.Store(true)
		return nil
	}

	mgr := NewDirectKnockManager(wakeHandler, log)

	srv := &db.Server{
		ID:     "srv-wake-handler-test",
		Name:   "Test Wake Server",
		Port:   port,
		Status: db.StatusDeepSleep,
	}

	if err := mgr.StartKnock(srv); err != nil {
		t.Fatalf("StartKnock failed: %v", err)
	}
	defer mgr.StopAll()

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	handshake := buildTestHandshakePacket(765, "127.0.0.1", uint16(port), 2)
	if _, err := conn.Write(handshake); err != nil {
		t.Fatalf("failed to write handshake: %v", err)
	}

	// Wait up to 1 second for wakeHandler to be called
	deadline := time.Now().Add(time.Second)
	for !woke.Load() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if !woke.Load() {
		t.Fatalf("expected wakeHandler to be called on login intent")
	}

	mu.Lock()
	defer mu.Unlock()
	if wokeServerID != srv.ID {
		t.Fatalf("expected wakeHandler to be called with %s, got %s", srv.ID, wokeServerID)
	}
}

// Test 5: StopAll cleanly closes multiple listeners
func TestDirectKnock_StopAll(t *testing.T) {
	port1 := getFreePort(t)
	port2 := getFreePort(t)
	log := logger.New()
	mgr := NewDirectKnockManager(nil, log)

	_ = mgr.StartKnock(&db.Server{ID: "srv-1", Port: port1, Status: db.StatusDeepSleep})
	_ = mgr.StartKnock(&db.Server{ID: "srv-2", Port: port2, Status: db.StatusDeepSleep})

	if !mgr.HasKnock("srv-1") || !mgr.HasKnock("srv-2") {
		t.Fatalf("expected both knock listeners to be active")
	}

	mgr.StopAll()

	if mgr.HasKnock("srv-1") || mgr.HasKnock("srv-2") {
		t.Fatalf("expected all knock listeners to be stopped")
	}
}
