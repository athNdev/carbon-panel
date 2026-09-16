#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.


# memory-sync.sh — single-writer qmd sync for the ACTIVE replica only.
# Usage (on the active replica):
#   ACTIVE_WRITER=1 ./scripts/memory-sync.sh
# The index.sqlite artifact is git-ignored; replicas sync via this script,
# never by committing the index.
set -euo pipefail

# (a) Refuse to run unless this host is the designated active writer.
if [ "${ACTIVE_WRITER:-0}" != "1" ]; then
  echo "Refusing: ACTIVE_WRITER != 1."
  echo "This script is the single-writer sync and must run on the ACTIVE replica only."
  echo "To run: ACTIVE_WRITER=1 $0"
  echo "Passive replicas: pull git, then ask the active replica owner to run this script."
  exit 1
fi

# (b) Install qmd if missing.
if ! command -v qmd >/dev/null 2>&1; then
  echo "qmd not found; installing via npm..."
  npm i -g @tobilu/qmd
fi

# (c) Update the project-local index (prefer the named collection).
qmd --index carbon-panel update || qmd update

# (d) Re-embed changed content.
qmd embed

# (e) Health print.
qmd status

# (f) Reminder: index artifacts are local-only.
echo "Done. Reminder: .qmd/index.sqlite is git-ignored and syncs via this script — never commit it."
