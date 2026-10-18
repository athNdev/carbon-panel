#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.


# Runs the SvelteKit production build with bounded heap and retry on transient
# SIGILL/SIGSEGV crashes (exit codes 132/139). Bun 1.3.x JIT on Linux x86_64
# occasionally trips an illegal instruction during heavy Vite/Rollup bundling;
# running vite via node when available avoids the Bun JIT crash path entirely,
# and bounding --max-old-space-size from MemAvailable prevents OOM on small nodes.
set -euo pipefail

cd "$(dirname "$0")/../web/carbon-panel"

MEM_KB="$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo 2>/dev/null || true)"
if [ -n "$MEM_KB" ]; then
	BUDGET_KB=$((MEM_KB * 55 / 100))
	[ "$BUDGET_KB" -lt 900000 ] && BUDGET_KB=900000
	[ "$BUDGET_KB" -gt 2621440 ] && BUDGET_KB=2621440
	HEAP_MB=$(((BUDGET_KB - 262144) / 1024))
	[ "$HEAP_MB" -lt 625 ] && HEAP_MB=625
	CAP="$HEAP_MB"
else
	CAP=1024
fi
EXIST="$(printf '%s' "${NODE_OPTIONS:-}" | sed -n 's/.*--max-old-space-size=\([0-9][0-9]*\).*/\1/p')"
if [ -n "$EXIST" ] && [ "$EXIST" -lt "$CAP" ]; then
	CAP="$EXIST"
fi
REST="$(printf '%s' "${NODE_OPTIONS:-}" | sed 's/--max-old-space-size=[0-9][0-9]*//g')"
export NODE_OPTIONS="--max-old-space-size=${CAP} ${REST}"

run_vite_build() {
	if command -v node >/dev/null 2>&1; then
		node ./node_modules/vite/bin/vite.js build
	else
		bun ./node_modules/vite/bin/vite.js build
	fi
}

max_attempts=3
for attempt in $(seq 1 "$max_attempts"); do
	set +e
	run_vite_build
	rc=$?
	set -e
	if [ "$rc" -eq 0 ]; then
		touch build/.gitkeep
		exit 0
	fi
	if [ "$rc" -ne 132 ] && [ "$rc" -ne 139 ]; then
		exit "$rc"
	fi
	echo "warn: vite build exited with signal code $rc (attempt $attempt/$max_attempts), retrying..." >&2
done

exit "$rc"
