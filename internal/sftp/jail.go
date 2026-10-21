// Package sftp provides a chrooted per-server SFTP entry point (MINE-190).
// Full protocol implementation requires an SFTP library; this package exposes
// the configuration and path-jail helpers available today.
package sftp

import (
	"fmt"
	"path"
	"strings"
)

// JailPath resolves target under root and rejects traversal outside root.
func JailPath(root, target string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("sftp: root required")
	}
	clean := path.Clean(target)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("sftp: path escapes jail")
	}
	full := path.Join(root, clean)
	if !strings.HasPrefix(full, root) {
		return "", fmt.Errorf("sftp: path escapes jail")
	}
	return full, nil
}

