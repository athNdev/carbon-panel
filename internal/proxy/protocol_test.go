package proxy

import (
	"bytes"
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
