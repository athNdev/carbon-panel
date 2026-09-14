#!/bin/bash

set -e

IMAGE_NAME="mineserver"
IMAGE_TAG="dev"
REGISTRY="athNdev"
FULL_IMAGE_NAME="${REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}"

echo "Building ${FULL_IMAGE_NAME}..."

docker build \
    -t "${FULL_IMAGE_NAME}" \
    -f docker/Dockerfile.mineserver \
    .

echo "Pushing ${FULL_IMAGE_NAME}..."
docker push "${FULL_IMAGE_NAME}"

echo "Build and push complete: ${FULL_IMAGE_NAME}"

