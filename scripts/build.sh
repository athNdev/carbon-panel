#!/bin/bash
# SPDX-License-Identifier: MIT AND MPL-2.0
# This file includes DiscoPanel material under the MIT License,
# Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
# and still applies to that inherited material.
# Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
# If a copy of the MPL was not distributed with this file, You can obtain
# one at https://mozilla.org/MPL/2.0/.


set -e

IMAGE_NAME="mineserver"
IMAGE_TAG="${1:-dev}"
REGISTRY="${DOCKER_REGISTRY:-athndev}"
FULL_IMAGE_NAME="${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}"

echo "Building ${FULL_IMAGE_NAME}..."

docker build \
    -t "${FULL_IMAGE_NAME}" \
    -f docker/Dockerfile.mineserver \
    .

if [ "$PUSH" = "true" ] || [ "$2" = "--push" ]; then
    echo "Pushing ${FULL_IMAGE_NAME}..."
    docker push "${FULL_IMAGE_NAME}"
    echo "Build and push complete: ${FULL_IMAGE_NAME}"
else
    echo "Build complete: ${FULL_IMAGE_NAME} (use --push or PUSH=true to push)"
fi
