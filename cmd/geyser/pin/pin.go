// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package pin downloads one checksummed Geyser standalone jar.
package pin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const (
	// URL is Geyser standalone 2.12.0 build 1265. The entrypoint must not
	// follow a floating "latest" URL (MINE-187).
	URL      = "https://download.geysermc.org/v2/projects/geyser/versions/2.12.0/builds/1265/downloads/standalone"
	SHA256   = "3e235b5112110665a53ed3b42c0b8d514021c50184c01380aa5b01d52ee1f636"
	MaxBytes = 80 << 20
	host     = "download.geysermc.org"
)

// Fetch writes rawURL to dest when the body matches wantSHA and every
// redirect stays on download.geysermc.org.
func Fetch(client *http.Client, rawURL, dest, wantSHA string, maxBytes int64) error {
	if client == nil {
		client = http.DefaultClient
	}
	cloned := *client
	baseCheck := client.CheckRedirect
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Host != host {
			return fmt.Errorf("refusing redirect off %s to %s", host, req.URL.Host)
		}
		if len(via) >= 3 {
			return fmt.Errorf("too many redirects")
		}
		if baseCheck != nil {
			return baseCheck(req, via)
		}
		return nil
	}

	resp, err := cloned.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "geyser-*.jar")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(resp.Body, maxBytes+1))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if n > maxBytes {
		return fmt.Errorf("geyser download exceeded %d bytes", maxBytes)
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	if sum != wantSHA {
		return fmt.Errorf("geyser checksum mismatch")
	}
	return os.Rename(tmpName, dest)
}
