// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// recoveryKeyEnv re-enables printing the recovery key to stderr.
//
// The key is a panel-wide, long-lived secret that bypasses normal
// authentication. Printing it on every start writes that secret into whatever
// captures the process output: container logs, `docker logs`, journald, a log
// shipper, or a CI log that anyone with cluster read access can page back
// through. So the default is off and the operator opts in explicitly.
const recoveryKeyEnv = "CARBON_PANEL_PRINT_RECOVERY_KEY"

// recoveryKeyFileName is the on-disk location, relative to the data directory.
const recoveryKeyFileName = "recovery.key"

// recoveryKeyPath returns where the recovery key is persisted.
func recoveryKeyPath(dataDir string) string {
	return filepath.Join(dataDir, recoveryKeyFileName)
}

// writeRecoveryKey persists the key with owner-only permissions and returns
// its path. It creates the data directory if needed so a fresh deployment does
// not lose the key on a missing-directory error.
func writeRecoveryKey(dataDir, key string) (string, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", fmt.Errorf("create data dir %q: %w", dataDir, err)
	}
	path := recoveryKeyPath(dataDir)
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		return "", fmt.Errorf("write recovery key to %q: %w", path, err)
	}
	return path, nil
}

// recoveryKeyPrintAllowed reports whether the secret itself may be echoed.
//
// `explicit` is the --print-recovery-key flag. `lookup` reads an environment
// variable; it is injected so the decision is testable without mutating the
// process environment. Both must agree on intent: an unparseable value is
// treated as "do not print" rather than "print".
func recoveryKeyPrintAllowed(explicit bool, lookup func(string) (string, bool)) bool {
	if explicit {
		return true
	}
	raw, ok := lookup(recoveryKeyEnv)
	if !ok {
		return false
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false
	}
	return enabled
}

// announceRecoveryKey reports where the key lives, and only prints the secret
// when allowPrint is true.
func announceRecoveryKey(w io.Writer, key, path string, allowPrint bool) {
	if key == "" {
		return
	}
	if !allowPrint {
		fmt.Fprintf(w, "Recovery key written to %s (not printed; set %s=1 or pass --print-recovery-key to display it)\n",
			path, recoveryKeyEnv)
		return
	}
	fmt.Fprintf(w, "\n--------------------------------------------------\n")
	fmt.Fprintf(w, "RECOVERY KEY (use to reset panel access if locked out)\n")
	fmt.Fprintf(w, "%s\n", key)
	fmt.Fprintf(w, "--------------------------------------------------\n\n")
}
