// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package backupcrypt implements the versioned authenticated encrypted
// backup artifact format (OSS-236). Artifacts are AES-256-GCM in chunked
// streaming mode so memory stays bounded for large worlds: every 64 KiB
// chunk is authenticated before it is written, the key ID is bound into the
// authentication data, and a truncated or tampered artifact always fails
// closed. The format is self-describing: magic, version and key ID precede
// the ciphertext so a restore can pick the right key and legacy plaintext
// archives stay recognizable.
package backupcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	magic   = "CARBONENC"
	Version = 1
	// chunkSize bounds the streaming buffers: each plaintext chunk is at
	// most this many bytes before sealing.
	chunkSize = 64 * 1024
	// keySize is the AES-256 key length in bytes.
	keySize = 32
	// noncePrefixSize is the per-artifact random nonce prefix; the chunk
	// counter completes the 12-byte GCM nonce.
	noncePrefixSize = 8
	maxKeyIDLen     = 1024
)

var (
	// ErrWrongKey reports a key that does not match the artifact's key ID or
	// fails authentication. No plaintext is produced.
	ErrWrongKey = errors.New("backup encryption key does not match the artifact")
	// ErrTampered reports an artifact that failed authentication or framing.
	ErrTampered = errors.New("encrypted backup artifact is corrupt or tampered")
	// ErrBadFormat reports a file that is not this artifact format.
	ErrBadFormat = errors.New("not an encrypted backup artifact")
	// ErrKeyMissing reports that encryption is expected but no key is
	// configured. The explicit disabled-encryption state is "no key".
	ErrKeyMissing = errors.New("backup is encrypted but no encryption key is configured")
)

// Key is a backup encryption key with its non-secret rotation label.
type Key struct {
	ID  string
	key []byte
}

// ParseKey decodes key material (base64 or hex, 32 bytes) and attaches the
// key ID recorded with every artifact. The key material is never logged.
func ParseKey(keyID, material string) (*Key, error) {
	id := strings.TrimSpace(keyID)
	if id == "" {
		return nil, errors.New("backup encryption key ID is required when a key is configured")
	}
	raw := strings.TrimSpace(material)
	var decoded []byte
	var err error
	if decoded, err = hex.DecodeString(raw); err != nil || len(decoded) != keySize {
		if decoded, err = base64.StdEncoding.DecodeString(raw); err != nil || len(decoded) != keySize {
			if decoded, err = base64.RawStdEncoding.DecodeString(raw); err != nil || len(decoded) != keySize {
				return nil, fmt.Errorf("backup encryption key must be %d bytes encoded as hex or base64", keySize)
			}
		}
	}
	return &Key{ID: id, key: decoded}, nil
}

// ResolveKey returns the configured key, or nil when encryption is
// explicitly disabled (no key material configured).
func ResolveKey(keyID, material string) (*Key, error) {
	if strings.TrimSpace(material) == "" {
		return nil, nil
	}
	return ParseKey(keyID, material)
}

// IsEncrypted reports whether the file starts with the artifact magic.
func IsEncrypted(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, len(magic))
	if _, err := io.ReadFull(f, head); err != nil {
		return false, ErrBadFormat
	}
	return string(head) == magic, nil
}

// EncryptedKeyID returns the key ID recorded in an artifact header.
func EncryptedKeyID(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	id, _, _, err := readHeader(f)
	return id, err
}

func readHeader(r io.Reader) (keyID string, noncePrefix []byte, aead cipher.AEAD, err error) {
	head := make([]byte, len(magic)+1+2)
	if _, err = io.ReadFull(r, head); err != nil {
		return "", nil, nil, ErrBadFormat
	}
	if string(head[:len(magic)]) != magic {
		return "", nil, nil, ErrBadFormat
	}
	if head[len(magic)] != Version {
		return "", nil, nil, fmt.Errorf("%w: unsupported artifact version %d", ErrBadFormat, head[len(magic)])
	}
	idLen := int(binary.BigEndian.Uint16(head[len(magic)+1:]))
	if idLen == 0 || idLen > maxKeyIDLen {
		return "", nil, nil, ErrBadFormat
	}
	idBytes := make([]byte, idLen)
	if _, err = io.ReadFull(r, idBytes); err != nil {
		return "", nil, nil, ErrBadFormat
	}
	noncePrefix = make([]byte, noncePrefixSize)
	if _, err = io.ReadFull(r, noncePrefix); err != nil {
		return "", nil, nil, ErrBadFormat
	}
	return string(idBytes), noncePrefix, nil, nil
}

// headerBytes builds the artifact header; its bytes are the associated data
// authenticated with every chunk, binding version and key ID to the content.
func headerBytes(keyID string, noncePrefix []byte) []byte {
	id := []byte(keyID)
	out := make([]byte, 0, len(magic)+1+2+len(id)+len(noncePrefix))
	out = append(out, magic...)
	out = append(out, Version)
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(id)))
	out = append(out, lenBuf[:]...)
	out = append(out, id...)
	out = append(out, noncePrefix...)
	return out
}

func newAEAD(key *Key) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key.key)
	if err != nil {
		return nil, fmt.Errorf("backup encryption key is unusable: %w", err)
	}
	return cipher.NewGCM(block)
}

func chunkNonce(prefix []byte, counter uint32) []byte {
	nonce := make([]byte, 0, noncePrefixSize+4)
	nonce = append(nonce, prefix...)
	var c [4]byte
	binary.BigEndian.PutUint32(c[:], counter)
	return append(nonce, c[:]...)
}

// EncryptFile writes srcPath to dstPath as a versioned authenticated
// artifact under key. Streaming and bounded: at most one chunk is held in
// memory at a time.
func EncryptFile(srcPath, dstPath string, key *Key) error {
	if key == nil {
		return ErrKeyMissing
	}
	aead, err := newAEAD(key)
	if err != nil {
		return err
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	noncePrefix := make([]byte, noncePrefixSize)
	if _, err := rand.Read(noncePrefix); err != nil {
		return fmt.Errorf("backup encryption nonce: %w", err)
	}
	header := headerBytes(key.ID, noncePrefix)
	if _, err := dst.Write(header); err != nil {
		return err
	}

	buf := make([]byte, chunkSize)
	for counter := uint32(0); ; counter++ {
		n, rerr := io.ReadFull(src, buf)
		if rerr != nil && rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
			return rerr
		}
		last := rerr == io.EOF || rerr == io.ErrUnexpectedEOF
		if n == 0 && !last {
			// io.ReadFull returns n==0 only on EOF; belt and braces.
			last = true
		}
		if n > 0 {
			sealed := aead.Seal(nil, chunkNonce(noncePrefix, counter), buf[:n], header)
			var lenBuf [4]byte
			binary.BigEndian.PutUint32(lenBuf[:], uint32(len(sealed)))
			if _, err := dst.Write(lenBuf[:]); err != nil {
				return err
			}
			if _, err := dst.Write(sealed); err != nil {
				return err
			}
		}
		if last {
			break
		}
	}
	// Explicit terminator: truncation is detectable even when the final
	// chunk length would otherwise look complete.
	var zero [4]byte
	if _, err := dst.Write(zero[:]); err != nil {
		return err
	}
	return dst.Sync()
}

// DecryptFile verifies and decrypts srcPath into dstPath under key. Every
// chunk is authenticated before it is written and the whole artifact must
// verify, so a wrong key or tampering produces no usable plaintext and never
// an unauthenticated write.
func DecryptFile(srcPath, dstPath string, key *Key) error {
	if key == nil {
		return ErrKeyMissing
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	keyID, noncePrefix, _, err := readHeader(src)
	if err != nil {
		return err
	}
	if keyID != key.ID {
		return fmt.Errorf("%w: artifact wants key %q", ErrWrongKey, keyID)
	}
	aead, err := newAEAD(key)
	if err != nil {
		return err
	}
	header := headerBytes(keyID, noncePrefix)

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	plain := make([]byte, 0, chunkSize+aead.Overhead())
	for counter := uint32(0); ; counter++ {
		var lenBuf [4]byte
		if _, err := io.ReadFull(src, lenBuf[:]); err != nil {
			if err == io.EOF {
				return ErrTampered // missing terminator
			}
			return ErrTampered
		}
		n := int(binary.BigEndian.Uint32(lenBuf[:]))
		if n == 0 {
			// Terminator reached; trailing garbage is tampering.
			var extra [1]byte
			if m, _ := src.Read(extra[:]); m > 0 {
				return ErrTampered
			}
			return dst.Sync()
		}
		if n < aead.Overhead() || n > chunkSize+aead.Overhead() {
			return ErrTampered
		}
		sealed := make([]byte, n)
		if _, err := io.ReadFull(src, sealed); err != nil {
			return ErrTampered
		}
		plain, err = aead.Open(plain[:0], chunkNonce(noncePrefix, counter), sealed, header)
		if err != nil {
			return fmt.Errorf("%w: chunk %d failed authentication", ErrTampered, counter)
		}
		if _, err := dst.Write(plain); err != nil {
			return err
		}
	}
}

// Fingerprint returns a short one-way fingerprint of the key material for
// operational identification. It never reveals key bytes.
func (k *Key) Fingerprint() string {
	sum := sha256.Sum256(k.key)
	return hex.EncodeToString(sum[:8])
}
