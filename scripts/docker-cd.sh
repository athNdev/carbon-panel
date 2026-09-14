#!/bin/bash
# ==============================================================================
# Carbon Panel Docker Hub Continuous Deployment (CD) Script
#
# Builds and pushes multi-architecture or local Docker images to Docker Hub.
#
# Usage:
#   ./scripts/docker-cd.sh [TAG] [--push] [--multiarch]
#
# Environment variables:
#   DOCKER_HUB_USERNAME / DOCKERHUB_USERNAME  (default: athndev)
#   DOCKER_HUB_PASSWORD / DOCKERHUB_TOKEN     (optional, logs in if set)
#   PLATFORMS                                 (default: linux/amd64,linux/arm64)
# ==============================================================================

set -e

TAG="${1:-latest}"
REGISTRY="${DOCKER_HUB_USERNAME:-${DOCKERHUB_USERNAME:-athndev}}"
IMAGE="${REGISTRY}/carbon-panel:${TAG}"
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"

PUSH=false
MULTIARCH=false

for arg in "$@"; do
    case "$arg" in
        --push) PUSH=true ;;
        --multiarch) MULTIARCH=true ;;
    esac
done

echo "=== Carbon Panel Docker CD ==="
echo "Target Image: ${IMAGE}"
echo "Registry:     ${REGISTRY}"
echo "Platforms:    ${PLATFORMS}"
echo "Push enabled: ${PUSH}"
echo "============================"

# Authenticate if password/token provided in env
PASSWORD="${DOCKER_HUB_PASSWORD:-${DOCKERHUB_TOKEN:-}}"
if [ -n "$PASSWORD" ]; then
    echo "Authenticating to Docker Hub as ${REGISTRY}..."
    echo "$PASSWORD" | docker login --username "$REGISTRY" --password-stdin
fi

if [ "$MULTIARCH" = "true" ] || [ "$PUSH" = "true" ]; then
    echo "Building multi-architecture image with buildx..."
    BUILDX_CMD=(docker buildx build --platform "$PLATFORMS" --file docker/Dockerfile.carbon-panel -t "$IMAGE")
    if [ "$PUSH" = "true" ]; then
        BUILDX_CMD+=(--push)
    else
        BUILDX_CMD+=(--load)
    fi
    "${BUILDX_CMD[@]}" .
else
    echo "Building local Docker image..."
    docker build -f docker/Dockerfile.carbon-panel -t "$IMAGE" .
fi

echo "=== Carbon Panel Docker CD Complete ==="
