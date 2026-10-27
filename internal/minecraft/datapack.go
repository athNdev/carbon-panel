// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package minecraft

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/athNdev/carbon-panel/pkg/utils"
)

// DefaultLevelName is the world directory Minecraft uses when server.properties
// does not override level-name.
const DefaultLevelName = "world"

// MaxDatapackBytes caps a downloaded datapack archive. Datapacks are small
// world-data bundles; anything enormous is a mistake or an attack.
const MaxDatapackBytes = 256 << 20 // 256 MiB

// GetDatapacksPath returns the datapack directory for a server's world. Datapacks
// are world data, not mods, so they live under the world folder rather than a
// loader-specific mods directory.
func GetDatapacksPath(serverDataPath, levelName string) string {
	level := strings.TrimSpace(levelName)
	// level-name must be a plain directory name. Reject anything with a path
	// separator or leading dot so it can never escape the server root, on any OS.
	if level == "" || strings.HasPrefix(level, ".") || strings.ContainsAny(level, `/\\`) {
		level = DefaultLevelName
	}
	return filepath.Join(serverDataPath, level, "datapacks")
}

// IsValidDatapackFile reports whether a filename has a datapack archive
// extension. Datapacks are distributed as ZIP archives.
func IsValidDatapackFile(filename string) bool {
	return strings.EqualFold(filepath.Ext(filename), ".zip")
}

// ValidateDatapackArchive checks that a file is a well-formed datapack: a ZIP
// archive containing a pack.mcmeta descriptor at its root. This is what
// separates a real datapack from an arbitrary zip a user might upload.
func ValidateDatapackArchive(path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("not a valid zip archive: %w", err)
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		name := strings.TrimPrefix(filepath.ToSlash(f.Name), "./")
		if name == "pack.mcmeta" {
			return nil
		}
	}
	return fmt.Errorf("archive has no pack.mcmeta, so it is not a Minecraft datapack")
}

// hasDotDotSegment reports whether any path segment is "..", i.e. a traversal.
func hasDotDotSegment(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// NormalizeDatapackArchive repairs a common real-world packaging mistake: the
// whole datapack is nested inside a single wrapper directory (pack.mcmeta sits
// at wrapper/pack.mcmeta instead of the archive root). Minecraft ignores such
// archives, so the installer re-packs them with the wrapper stripped.
// It reports whether the archive was rewritten. An archive that is already
// valid, or that cannot be repaired, is left untouched (an error is returned
// only for a structurally broken archive).
func NormalizeDatapackArchive(path string) (bool, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return false, fmt.Errorf("not a valid zip archive: %w", err)
	}
	defer func() { _ = r.Close() }()

	wrapper := ""
	rootMeta := false
	for _, f := range r.File {
		name := strings.TrimPrefix(filepath.ToSlash(f.Name), "./")
		parts := strings.Split(name, "/")
		if name == "pack.mcmeta" && !f.FileInfo().IsDir() {
			rootMeta = true
		}
		if len(parts) == 2 && parts[1] == "pack.mcmeta" && !f.FileInfo().IsDir() {
			if wrapper != "" && wrapper != parts[0] {
				return false, fmt.Errorf("archive has multiple top-level folders and no root pack.mcmeta")
			}
			wrapper = parts[0]
		}
		if strings.HasPrefix(name, "/") || hasDotDotSegment(name) {
			return false, fmt.Errorf("archive contains unsafe entry %q", name)
		}
	}
	if rootMeta {
		return false, nil // already a well-formed datapack; nothing to repair
	}
	if wrapper == "" {
		return false, fmt.Errorf("archive has no pack.mcmeta, so it is not a Minecraft datapack")
	}

	// Rewrite to a sibling temp file, stripping the wrapper prefix, then
	// atomically swap it in. Streaming keeps memory flat for large packs.
	tmp := path + ".normalize.tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return false, fmt.Errorf("create temp archive: %w", err)
	}
	w := zip.NewWriter(out)
	ok := false
	defer func() {
		_ = w.Close()
		_ = out.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()

	prefix := wrapper + "/"
	for _, f := range r.File {
		name := strings.TrimPrefix(filepath.ToSlash(f.Name), "./")
		if name == wrapper || !strings.HasPrefix(name, prefix) {
			continue // the wrapper dir entry itself, or anything outside it
		}
		head := f.FileHeader
		head.Name = strings.TrimPrefix(name, prefix)
		if head.Name == "" {
			continue
		}
		dst, err := w.CreateHeader(&head)
		if err != nil {
			return false, fmt.Errorf("rewrite entry %q: %w", name, err)
		}
		src, err := f.Open()
		if err != nil {
			return false, fmt.Errorf("read entry %q: %w", name, err)
		}
		_, err = io.Copy(dst, src)
		_ = src.Close()
		if err != nil {
			return false, fmt.Errorf("copy entry %q: %w", name, err)
		}
	}
	if err := w.Close(); err != nil {
		return false, fmt.Errorf("finalize archive: %w", err)
	}
	if err := out.Close(); err != nil {
		return false, fmt.Errorf("close archive: %w", err)
	}
	// Close the source before renaming over it: on Windows the rename fails
	// while the file is still open.
	_ = r.Close()

	if err := os.Rename(tmp, path); err != nil {
		return false, fmt.Errorf("replace archive: %w", err)
	}
	ok = true
	return true, nil
}

// ValidateDatapackFilename rejects path traversal and non-zip names.
func ValidateDatapackFilename(name string) error {
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) ||
		filepath.VolumeName(name) != "" || strings.ContainsAny(name, `/\`) ||
		filepath.Base(name) != name {
		return errors.New("invalid datapack file name")
	}
	if !IsValidDatapackFile(name) {
		return errors.New("datapack must be a .zip archive")
	}
	return nil
}

// DownloadAllowsPrivate reports whether downloads driven by an indexer at
// indexerURL may target private network addresses. A public indexer's
// responses must never point the panel at internal hosts (SSRF via tampered
// index data), while a self-hosted indexer on a private network is an
// operator's deliberate trust decision and keeps serving private downloads.
// Unresolvable indexer hosts fail closed to strict (public-only).
func DownloadAllowsPrivate(indexerURL string) bool {
	u, err := url.Parse(indexerURL)
	if err != nil || u.Hostname() == "" {
		return false
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		return ip.IsPrivate() || ip.IsLoopback()
	}
	ips, err := net.LookupIP(u.Hostname())
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if ip.IsPrivate() || ip.IsLoopback() {
			return true
		}
	}
	return false
}

// DownloadDatapackArchiveFrom streams a datapack to destPath with an
// SSRF-guarded client whose private-network allowance is scoped to the
// indexer that supplied the URL. It does not trust Content-Length.
func DownloadDatapackArchiveFrom(ctx context.Context, indexerURL, rawURL, destPath string) error {
	return DownloadDatapackArchiveWithClient(utils.NewSafeHTTPClient(5*time.Minute, DownloadAllowsPrivate(indexerURL)), ctx, rawURL, destPath)
}

// DownloadDatapackArchiveWithClient is DownloadDatapackArchive with an explicit
// client so tests can exercise the streaming and size logic without defeating
// the SSRF guard.
func DownloadDatapackArchiveWithClient(client *http.Client, ctx context.Context, rawURL, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "CarbonPanel/2.x (Datapack Installer)")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("remote server returned status %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	defer func() { _ = out.Close() }()

	written, err := io.Copy(out, io.LimitReader(resp.Body, MaxDatapackBytes+1))
	if err != nil {
		// Close before removing: on Windows a remove of an open file fails.
		_ = out.Close()
		_ = os.Remove(destPath)
		return fmt.Errorf("write dest: %w", err)
	}
	if written > MaxDatapackBytes {
		_ = out.Close()
		_ = os.Remove(destPath)
		return fmt.Errorf("datapack exceeds %d bytes", MaxDatapackBytes)
	}
	return nil
}
