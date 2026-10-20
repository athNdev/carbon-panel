// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package files

import (
	"path"
	"path/filepath"
	"strings"
)

// PathExcluded reports whether a relative archive path matches a glob or
// sits under an excluded directory. Patterns use forward slashes.
func PathExcluded(rel string, patterns []string) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." || rel == "" {
		return false
	}
	base := path.Base(rel)
	for _, raw := range patterns {
		p := filepath.ToSlash(strings.TrimSpace(raw))
		p = strings.TrimPrefix(p, "./")
		if p == "" || p == "." {
			continue
		}
		if ok, err := path.Match(p, rel); err == nil && ok {
			return true
		}
		if !strings.Contains(p, "/") {
			if ok, err := path.Match(p, base); err == nil && ok {
				return true
			}
		}
		dir := strings.TrimSuffix(p, "/")
		if rel == dir || strings.HasPrefix(rel, dir+"/") {
			return true
		}
	}
	return false
}
