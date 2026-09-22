#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.


# Generates the self-signed certificate the local Authelia dev compose mounts.
#
# The private key is deliberately not committed (see .gitignore), so a fresh
# clone must run this once before `docker compose -f oidc/authelia/docker-compose.yaml up`.
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/config"
mkdir -p "$DIR"

openssl req -x509 -nodes -newkey rsa:2048 -days 3650 \
  -keyout "$DIR/tls.key" \
  -out "$DIR/tls.crt" \
  -subj "/CN=authelia.traefik.me" \
  -addext "subjectAltName=DNS:authelia.traefik.me,DNS:localhost"

chmod 600 "$DIR/tls.key"
echo "wrote $DIR/tls.key and $DIR/tls.crt"
