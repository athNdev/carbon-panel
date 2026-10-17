package provision

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateWorkspace rejects workspace names that could escape the root:
// empty names, absolute paths, ".." segments and embedded separators that
// resolve outside the root. Valid names are DNS-ish: [a-z0-9-_], plus "."
// only as BinderHub-style single dots are rejected too (keep it strict).
func ValidateWorkspace(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty", ErrInvalidWorkspace)
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("%w: absolute path %q", ErrInvalidWorkspace, name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("%w: traversal in %q", ErrInvalidWorkspace, name)
	}
	clean := filepath.Clean(name)
	if clean != name || clean == "." {
		return fmt.Errorf("%w: not a plain name %q", ErrInvalidWorkspace, name)
	}
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !ok {
			return fmt.Errorf("%w: bad character in %q", ErrInvalidWorkspace, name)
		}
	}
	return nil
}

// WorkspaceDir joins a validated workspace name onto root and guarantees the
// result stays inside root.
func WorkspaceDir(root, workspace string) (string, error) {
	if err := ValidateWorkspace(workspace); err != nil {
		return "", err
	}
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("%w: empty root", ErrInvalidWorkspace)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidWorkspace, err)
	}
	joined := filepath.Join(abs, workspace)
	rel, err := filepath.Rel(abs, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: escape for %q", ErrInvalidWorkspace, workspace)
	}
	return joined, nil
}
