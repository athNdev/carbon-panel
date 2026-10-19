// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package proxy

import (
	"bytes"
	"strings"
	"testing"
)

// TestReadHandshakePacket_MaliciousAddressLength is a regression test for a
// remotely-triggerable crash: a crafted handshake packet can encode an
// address-length VarInt that decodes to a negative int32 (5-byte VarInt with
// all lower bits set), which would previously panic in
// make([]byte, addressLen) before ReadHandshakePacket ever validated it.
// Since handleConnection processes each client on its own goroutine with no
// recover(), an unrecovered panic here crashes the entire process.
func TestReadHandshakePacket_MaliciousAddressLength(t *testing.T) {
	// packetID=0x00, protocolVersion=0x2F(47), addressLen VarInt bytes that
	// decode to -1 (0xFFFFFFFF): 0xFF 0xFF 0xFF 0xFF 0x0F
	inner := []byte{0x00, 0x2F, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F}
	packet := append([]byte{byte(len(inner))}, inner...)

	_, err := ReadHandshakePacket(bytes.NewReader(packet))
	if err == nil {
		t.Fatal("expected error for negative address length, got nil (should not panic or succeed)")
	}
}

// TestReadHandshakePacket_AddressLengthExceedsBuffer ensures an address
// length that exceeds the remaining packet buffer is rejected up front
// instead of driving a large allocation from attacker-controlled input.
func TestReadHandshakePacket_AddressLengthExceedsBuffer(t *testing.T) {
	// packetID=0x00, protocolVersion=0x00, addressLen VarInt = 200 (0xC8 0x01)
	// but no address bytes actually follow.
	inner := []byte{0x00, 0x00, 0xC8, 0x01}
	packet := append([]byte{byte(len(inner))}, inner...)

	_, err := ReadHandshakePacket(bytes.NewReader(packet))
	if err == nil {
		t.Fatal("expected error for address length exceeding remaining buffer, got nil")
	}
}

// TestReadHandshakePacket_Valid confirms a well-formed handshake still parses.
func TestReadHandshakePacket_Valid(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteHandshakePacket(&buf, &HandshakePacket{
		ProtocolVersion: 47,
		ServerAddress:   "play.example.com",
		ServerPort:      25565,
		NextState:       1,
	}); err != nil {
		t.Fatalf("failed to write handshake packet: %v", err)
	}

	pkt, err := ReadHandshakePacket(&buf)
	if err != nil {
		t.Fatalf("failed to read valid handshake packet: %v", err)
	}
	if pkt.ServerAddress != "play.example.com" || pkt.ServerPort != 25565 || pkt.NextState != 1 {
		t.Errorf("unexpected parsed packet: %+v", pkt)
	}
}

// TestReadHandshakePacket_ExtendedForgeModpack confirms handshakes exceeding
// 255 bytes (such as Forge/NeoForge/Fabric modpack handshakes embedding FML tokens)
// are accepted up to MaxHandshakePacketLength.
func TestReadHandshakePacket_ExtendedForgeModpack(t *testing.T) {
	longAddress := "play.modpack.example.com\x00FML3\x00" + strings.Repeat("mod-channel-token;", 40)
	if len(longAddress) <= 255 {
		t.Fatalf("expected test address length > 255, got %d", len(longAddress))
	}

	var buf bytes.Buffer
	if err := WriteHandshakePacket(&buf, &HandshakePacket{
		ProtocolVersion: 765,
		ServerAddress:   longAddress,
		ServerPort:      25565,
		NextState:       1,
	}); err != nil {
		t.Fatalf("failed to write handshake packet: %v", err)
	}

	pkt, err := ReadHandshakePacket(&buf)
	if err != nil {
		t.Fatalf("expected extended handshake packet to parse successfully, got: %v", err)
	}
	if pkt.ServerAddress != longAddress || pkt.ServerPort != 25565 || pkt.NextState != 1 {
		t.Errorf("unexpected parsed packet: %+v", pkt)
	}
}
