package proxy

import (
	"bytes"
	"net"
	"testing"
	"time"
)

type dummyConn struct {
	net.Conn
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	remote   net.Addr
}

func (d *dummyConn) Read(b []byte) (int, error) {
	return d.readBuf.Read(b)
}

func (d *dummyConn) Write(b []byte) (int, error) {
	return d.writeBuf.Write(b)
}

func (d *dummyConn) Close() error {
	return nil
}

func (d *dummyConn) RemoteAddr() net.Addr {
	return d.remote
}

func (d *dummyConn) SetReadDeadline(t time.Time) error {
	return nil
}

func TestProxyV2Header_IPv4_RoundTrip(t *testing.T) {
	srcAddr := &net.TCPAddr{IP: net.ParseIP("203.0.113.195"), Port: 54321}
	dstAddr := &net.TCPAddr{IP: net.ParseIP("198.51.100.1"), Port: 25565}

	buf := new(bytes.Buffer)
	if err := WriteProxyV2Header(buf, srcAddr, dstAddr); err != nil {
		t.Fatalf("WriteProxyV2Header failed: %v", err)
	}

	header, err := ParseProxyV2Header(buf)
	if err != nil {
		t.Fatalf("ParseProxyV2Header failed: %v", err)
	}

	if header.Version != 2 {
		t.Errorf("expected version 2, got %d", header.Version)
	}
	if header.Command != CommandProxy {
		t.Errorf("expected CommandProxy, got %d", header.Command)
	}
	if header.SrcAddr == nil || !header.SrcAddr.IP.Equal(srcAddr.IP) || header.SrcAddr.Port != srcAddr.Port {
		t.Errorf("expected SrcAddr %v, got %v", srcAddr, header.SrcAddr)
	}
	if header.DstAddr == nil || !header.DstAddr.IP.Equal(dstAddr.IP) || header.DstAddr.Port != dstAddr.Port {
		t.Errorf("expected DstAddr %v, got %v", dstAddr, header.DstAddr)
	}
}

func TestProxyV2Header_IPv6_RoundTrip(t *testing.T) {
	srcAddr := &net.TCPAddr{IP: net.ParseIP("2001:db8:85a3::8a2e:370:7334"), Port: 49152}
	dstAddr := &net.TCPAddr{IP: net.ParseIP("2001:db8:1234::1"), Port: 25565}

	buf := new(bytes.Buffer)
	if err := WriteProxyV2Header(buf, srcAddr, dstAddr); err != nil {
		t.Fatalf("WriteProxyV2Header failed: %v", err)
	}

	header, err := ParseProxyV2Header(buf)
	if err != nil {
		t.Fatalf("ParseProxyV2Header failed: %v", err)
	}

	if header.Version != 2 {
		t.Errorf("expected version 2, got %d", header.Version)
	}
	if header.SrcAddr == nil || !header.SrcAddr.IP.Equal(srcAddr.IP) || header.SrcAddr.Port != srcAddr.Port {
		t.Errorf("expected SrcAddr %v, got %v", srcAddr, header.SrcAddr)
	}
}

func TestWrapConnWithProxyProtocol_WithHeader(t *testing.T) {
	srcAddr := &net.TCPAddr{IP: net.ParseIP("198.51.100.42"), Port: 34567}
	dstAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 25565}

	buf := new(bytes.Buffer)
	_ = WriteProxyV2Header(buf, srcAddr, dstAddr)
	// Append some Minecraft handshake payload bytes
	buf.WriteString("hello minecraft payload")

	dummy := &dummyConn{
		readBuf:  buf,
		writeBuf: new(bytes.Buffer),
		remote:   &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 12345}, // upstream load balancer IP
	}

	wrapped, hdr, err := WrapConnWithProxyProtocol(dummy)
	if err != nil {
		t.Fatalf("WrapConnWithProxyProtocol failed: %v", err)
	}
	if hdr == nil {
		t.Fatalf("expected parsed proxy header, got nil")
	}

	// RemoteAddr should return the real client IP (198.51.100.42), not the balancer (10.0.0.1)
	if wrapped.RemoteAddr().String() != "198.51.100.42:34567" {
		t.Errorf("expected remote addr 198.51.100.42:34567, got %s", wrapped.RemoteAddr().String())
	}

	// Remaining payload should be readable untouched
	payload := make([]byte, 23)
	n, _ := wrapped.Read(payload)
	if string(payload[:n]) != "hello minecraft payload" {
		t.Errorf("expected untouched payload, got %s", string(payload[:n]))
	}
}

func TestWrapConnWithProxyProtocol_WithoutHeader(t *testing.T) {
	buf := bytes.NewBufferString("regular minecraft handshake bytes")
	dummy := &dummyConn{
		readBuf:  buf,
		writeBuf: new(bytes.Buffer),
		remote:   &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 55555},
	}

	wrapped, hdr, err := WrapConnWithProxyProtocol(dummy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hdr != nil {
		t.Errorf("expected nil header for regular connection, got %v", hdr)
	}

	if wrapped.RemoteAddr().String() != "192.168.1.100:55555" {
		t.Errorf("expected original remote addr, got %s", wrapped.RemoteAddr().String())
	}

	payload := make([]byte, 33)
	n, _ := wrapped.Read(payload)
	if string(payload[:n]) != "regular minecraft handshake bytes" {
		t.Errorf("payload corrupted: got %s", string(payload[:n]))
	}
}
