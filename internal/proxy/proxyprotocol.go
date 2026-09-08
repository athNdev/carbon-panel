package proxy

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
)

var (
	// ProxyV2Signature is the 12-byte magic prefix for PROXY protocol v2
	ProxyV2Signature = []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}

	ErrTruncatedHeader = errors.New("truncated PROXY protocol v2 header")
	ErrInvalidVersion  = errors.New("unsupported PROXY protocol version (must be v2)")
)

const (
	CommandLocal byte = 0x00
	CommandProxy byte = 0x01

	FamilyUnspec byte = 0x00
	FamilyIPv4   byte = 0x10
	FamilyIPv6   byte = 0x20
	FamilyUnix   byte = 0x30

	TransportUnspec byte = 0x00
	TransportStream byte = 0x01
	TransportDgram  byte = 0x02
)

// ProxyHeader contains the parsed PROXY protocol v2 details
type ProxyHeader struct {
	Version   byte
	Command   byte
	Transport byte
	SrcAddr   *net.TCPAddr
	DstAddr   *net.TCPAddr
}

// ParseProxyV2Header reads and parses a PROXY protocol v2 header from the reader
func ParseProxyV2Header(r io.Reader) (*ProxyHeader, error) {
	// First 16 bytes: 12 bytes signature + 1 byte ver/cmd + 1 byte fam/transport + 2 bytes length
	var headerBuf [16]byte
	if _, err := io.ReadFull(r, headerBuf[:]); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTruncatedHeader, err)
	}

	if !bytes.Equal(headerBuf[:12], ProxyV2Signature) {
		return nil, fmt.Errorf("invalid PROXY protocol v2 signature")
	}

	verCmd := headerBuf[12]
	version := verCmd >> 4
	if version != 2 {
		return nil, fmt.Errorf("%w: got version %d", ErrInvalidVersion, version)
	}
	command := verCmd & 0x0F

	famTrans := headerBuf[13]
	payloadLen := binary.BigEndian.Uint16(headerBuf[14:16])

	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, fmt.Errorf("%w: reading payload (%v)", ErrTruncatedHeader, err)
		}
	}

	header := &ProxyHeader{
		Version:   version,
		Command:   command,
		Transport: famTrans,
	}

	if command == CommandLocal {
		return header, nil
	}

	// PROXY command - parse address family
	switch famTrans {
	case FamilyIPv4 | TransportStream:
		if len(payload) >= 12 {
			srcIP := net.IP(payload[0:4])
			dstIP := net.IP(payload[4:8])
			srcPort := int(binary.BigEndian.Uint16(payload[8:10]))
			dstPort := int(binary.BigEndian.Uint16(payload[10:12]))

			header.SrcAddr = &net.TCPAddr{IP: srcIP, Port: srcPort}
			header.DstAddr = &net.TCPAddr{IP: dstIP, Port: dstPort}
		}
	case FamilyIPv6 | TransportStream:
		if len(payload) >= 36 {
			srcIP := net.IP(payload[0:16])
			dstIP := net.IP(payload[16:32])
			srcPort := int(binary.BigEndian.Uint16(payload[32:34]))
			dstPort := int(binary.BigEndian.Uint16(payload[34:36]))

			header.SrcAddr = &net.TCPAddr{IP: srcIP, Port: srcPort}
			header.DstAddr = &net.TCPAddr{IP: dstIP, Port: dstPort}
		}
	}

	return header, nil
}

// WriteProxyV2Header writes a PROXY protocol v2 header to the writer for preserving client IP
func WriteProxyV2Header(w io.Writer, srcAddr, dstAddr net.Addr) error {
	var srcTCP, dstTCP *net.TCPAddr
	if tcp, ok := srcAddr.(*net.TCPAddr); ok {
		srcTCP = tcp
	} else if host, portStr, err := net.SplitHostPort(srcAddr.String()); err == nil {
		var port int
		fmt.Sscanf(portStr, "%d", &port)
		srcTCP = &net.TCPAddr{IP: net.ParseIP(host), Port: port}
	}

	if tcp, ok := dstAddr.(*net.TCPAddr); ok {
		dstTCP = tcp
	} else if host, portStr, err := net.SplitHostPort(dstAddr.String()); err == nil {
		var port int
		fmt.Sscanf(portStr, "%d", &port)
		dstTCP = &net.TCPAddr{IP: net.ParseIP(host), Port: port}
	}

	if srcTCP == nil || srcTCP.IP == nil {
		srcTCP = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0}
	}
	if dstTCP == nil || dstTCP.IP == nil {
		dstTCP = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 25565}
	}

	buf := new(bytes.Buffer)
	buf.Write(ProxyV2Signature)

	isIPv6 := srcTCP.IP.To4() == nil

	if !isIPv6 {
		// IPv4 STREAM
		buf.WriteByte(0x21) // v2, PROXY
		buf.WriteByte(0x11) // IPv4, STREAM
		_ = binary.Write(buf, binary.BigEndian, uint16(12))

		src4 := srcTCP.IP.To4()
		dst4 := dstTCP.IP.To4()
		if dst4 == nil {
			dst4 = net.ParseIP("127.0.0.1").To4()
		}
		buf.Write(src4)
		buf.Write(dst4)
		_ = binary.Write(buf, binary.BigEndian, uint16(srcTCP.Port))
		_ = binary.Write(buf, binary.BigEndian, uint16(dstTCP.Port))
	} else {
		// IPv6 STREAM
		buf.WriteByte(0x21) // v2, PROXY
		buf.WriteByte(0x21) // IPv6, STREAM
		_ = binary.Write(buf, binary.BigEndian, uint16(36))

		src16 := srcTCP.IP.To16()
		dst16 := dstTCP.IP.To16()
		if dst16 == nil {
			dst16 = net.ParseIP("::1").To16()
		}
		buf.Write(src16)
		buf.Write(dst16)
		_ = binary.Write(buf, binary.BigEndian, uint16(srcTCP.Port))
		_ = binary.Write(buf, binary.BigEndian, uint16(dstTCP.Port))
	}

	_, err := w.Write(buf.Bytes())
	return err
}

// ProxyConn wraps a net.Conn to intercept buffered reads and override RemoteAddr
type ProxyConn struct {
	net.Conn
	reader     io.Reader
	remoteAddr net.Addr
}

func (c *ProxyConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

func (c *ProxyConn) RemoteAddr() net.Addr {
	if c.remoteAddr != nil {
		return c.remoteAddr
	}
	return c.Conn.RemoteAddr()
}

// WrapConnWithProxyProtocol peeks the incoming connection for PROXY protocol v2 signature.
// If present, it consumes and parses the header, returning a wrapped connection with real client RemoteAddr.
// If absent, it transparently returns the connection without modifying the payload stream.
func WrapConnWithProxyProtocol(conn net.Conn) (net.Conn, *ProxyHeader, error) {
	br := bufio.NewReader(conn)
	peekBytes, err := br.Peek(12)
	if err == nil && bytes.Equal(peekBytes, ProxyV2Signature) {
		header, parseErr := ParseProxyV2Header(br)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		remote := conn.RemoteAddr()
		if header.SrcAddr != nil {
			remote = header.SrcAddr
		}
		return &ProxyConn{
			Conn:       conn,
			reader:     br,
			remoteAddr: remote,
		}, header, nil
	}

	return &ProxyConn{
		Conn:       conn,
		reader:     br,
		remoteAddr: conn.RemoteAddr(),
	}, nil, nil
}
