// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package backupcrypt

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testKey(t *testing.T, id, material string) *Key {
	t.Helper()
	k, err := ParseKey(id, material)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := testKey(t, "key-2026", strings.Repeat("ab", 32))
	for _, size := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 2*chunkSize + 7} {
		plain := bytes.Repeat([]byte{byte(size % 251)}, size)
		src := writeTemp(t, "plain.bin", plain)
		enc := filepath.Join(t.TempDir(), "artifact.enc")
		if err := EncryptFile(src, enc, key); err != nil {
			t.Fatalf("EncryptFile(%d): %v", size, err)
		}
		stored, err := os.ReadFile(enc)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.HasPrefix(stored, []byte("PK")) {
			t.Fatalf("stored artifact for %d bytes must not be a plain zip", size)
		}

		out := filepath.Join(t.TempDir(), "out.bin")
		if err := DecryptFile(enc, out, key); err != nil {
			t.Fatalf("DecryptFile(%d): %v", size, err)
		}
		got, err := os.ReadFile(out)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("round trip mismatch for %d bytes: %v", size, err)
		}
	}
}

func TestDecryptRejectsWrongAndMissingKeys(t *testing.T) {
	right := testKey(t, "key-2026", strings.Repeat("ab", 32))
	src := writeTemp(t, "plain.bin", []byte("world data"))
	enc := filepath.Join(t.TempDir(), "artifact.enc")
	if err := EncryptFile(src, enc, right); err != nil {
		t.Fatal(err)
	}

	// Missing key: refused before any plaintext exists.
	out := filepath.Join(t.TempDir(), "missing.bin")
	if err := DecryptFile(enc, out, nil); !errors.Is(err, ErrKeyMissing) {
		t.Fatalf("missing key error = %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("no plaintext may be produced without a key")
	}

	// Wrong key ID: refused by the header before decryption.
	wrongID := testKey(t, "key-old", strings.Repeat("cd", 32))
	out2 := filepath.Join(t.TempDir(), "wrongid.bin")
	if err := DecryptFile(enc, out2, wrongID); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("wrong key ID error = %v", err)
	}

	// Same key ID, wrong material: authentication must fail.
	wrongMaterial := testKey(t, "key-2026", strings.Repeat("cd", 32))
	out3 := filepath.Join(t.TempDir(), "wrongmat.bin")
	if err := DecryptFile(enc, out3, wrongMaterial); err == nil {
		t.Fatal("wrong key material must be rejected")
	}
}

func TestDecryptRejectsTamperedAndTruncatedArtifacts(t *testing.T) {
	key := testKey(t, "key-2026", strings.Repeat("ab", 32))
	src := writeTemp(t, "plain.bin", bytes.Repeat([]byte("world"), 30000))
	enc := filepath.Join(t.TempDir(), "artifact.enc")
	if err := EncryptFile(src, enc, key); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(enc)
	if err != nil {
		t.Fatal(err)
	}

	flip := append([]byte(nil), original...)
	flip[len(flip)/2] ^= 0xff
	broken := writeTemp(t, "flipped.enc", flip)
	if err := DecryptFile(broken, filepath.Join(t.TempDir(), "out1.bin"), key); !errors.Is(err, ErrTampered) {
		t.Fatalf("tampered artifact error = %v", err)
	}

	truncated := writeTemp(t, "truncated.enc", original[:len(original)-20])
	if err := DecryptFile(truncated, filepath.Join(t.TempDir(), "out2.bin"), key); err == nil {
		t.Fatal("truncated artifact must be rejected")
	}

	noTerminator := writeTemp(t, "noterm.enc", original[:len(original)-2])
	if err := DecryptFile(noTerminator, filepath.Join(t.TempDir(), "out3.bin"), key); err == nil {
		t.Fatal("artifact without terminator must be rejected")
	}

	// The key ID in the header is authenticated too.
	swapped := append([]byte(nil), original...)
	idLen := int(swapped[len(magic)+1])<<8 | int(swapped[len(magic)+2])
	if idLen < 2 {
		t.Fatal("test artifact key ID too short")
	}
	swapped[len(magic)+3+idLen-1] ^= 0x01
	tamperedID := writeTemp(t, "tamperedid.enc", swapped)
	if err := DecryptFile(tamperedID, filepath.Join(t.TempDir(), "out4.bin"), key); err == nil {
		t.Fatal("tampered key ID must be rejected")
	}
}

func TestIsEncryptedAndKeyID(t *testing.T) {
	key := testKey(t, "key-2026", strings.Repeat("ab", 32))
	src := writeTemp(t, "plain.bin", []byte("world data"))
	enc := filepath.Join(t.TempDir(), "artifact.enc")
	if err := EncryptFile(src, enc, key); err != nil {
		t.Fatal(err)
	}

	isEnc, err := IsEncrypted(enc)
	if err != nil || !isEnc {
		t.Fatalf("IsEncrypted = %v, %v", isEnc, err)
	}
	id, err := EncryptedKeyID(enc)
	if err != nil || id != "key-2026" {
		t.Fatalf("EncryptedKeyID = %q, %v", id, err)
	}

	plain := writeTemp(t, "plain.zip", []byte("PK\x03\x04 legacy plaintext"))
	isEnc, err = IsEncrypted(plain)
	if err != nil || isEnc {
		t.Fatalf("plaintext archive must not look encrypted: %v, %v", isEnc, err)
	}
	if _, err := EncryptedKeyID(plain); err == nil {
		t.Fatal("plaintext archive must not report a key ID")
	}
}

func TestParseKeyAndResolve(t *testing.T) {
	if _, err := ParseKey("", strings.Repeat("ab", 32)); err == nil {
		t.Fatal("key ID is required")
	}
	if _, err := ParseKey("key-1", "too-short"); err == nil {
		t.Fatal("short material must be rejected")
	}
	hexKey, err := ParseKey("key-1", strings.Repeat("ab", 32))
	if err != nil || len(hexKey.key) != keySize {
		t.Fatalf("hex key parse: %v", err)
	}
	b64Material := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xab}, keySize))
	b64Key, err := ParseKey("key-1", b64Material)
	if err != nil || len(b64Key.key) != keySize {
		t.Fatalf("base64 key parse: %v", err)
	}

	k, err := ResolveKey("key-1", "")
	if err != nil || k != nil {
		t.Fatalf("no material must mean explicitly disabled encryption: %v, %v", k, err)
	}
	if k, err := ResolveKey("key-1", strings.Repeat("ab", 32)); err != nil || k == nil {
		t.Fatalf("configured material must resolve: %v", err)
	}
}
