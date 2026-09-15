#!/bin/bash
# ==============================================================================
# Carbon Panel staging helper (discopanel-dev).
#
# Layout on the staging host:
#   /opt/staging/src            git clone of athNdev/carbon-panel
#   /opt/staging/docker-compose.yml   (copy of staging/docker-compose.yml)
#   /opt/staging/REF            currently deployed ref (branch or SHA)
#   /opt/staging/data|backups|tmp     staging-only data (never dev's)
#
# Usage:
#   staging.sh pin              deploy latest origin/main (default track)
#   staging.sh switch <branch>  deploy any branch for pre-merge testing
#   staging.sh autopin           cron entry: redeploy only if origin/main moved
#   staging.sh status           show deployed ref vs origin/main
#   staging.sh logs [n]         tail container logs
# ==============================================================================
set -e

SRC=/opt/staging/src
REF_FILE=/opt/staging/REF
IMAGE=carbon-panel:staging

need() { command -v "$1" >/dev/null || { echo "missing: $1"; exit 1; }; }
need git; need docker

deployed_ref() { cat "$REF_FILE" 2>/dev/null || echo "<none>"; }

deploy_ref() {
    local ref="$1"
    echo "=== staging deploy: $ref ==="
    git -C "$SRC" fetch origin --prune
    git -C "$SRC" checkout --detach "$ref"
    local sha
    sha=$(git -C "$SRC" rev-parse --short=12 HEAD)
    echo "building $IMAGE @ $sha ..."
    docker build -f "$SRC/docker/Dockerfile.carbon-panel" -t "$IMAGE" "$SRC"
    docker rm -f carbon-panel-staging 2>/dev/null || true
    # NOTE: plain docker run (this host has no compose plugin).
    # staging/docker-compose.yml documents the equivalent service.
    docker run -d --name carbon-panel-staging --restart unless-stopped \
      -p 8081:8080 \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -v /opt/staging/data:/app/data -v /opt/staging/backups:/app/backups -v /opt/staging/tmp:/app/tmp \
      -e CARBONPANEL_DATA_DIR=/app/data -e CARBONPANEL_HOST_DATA_PATH=/opt/staging/data -e TZ=UTC \
      --add-host host.docker.internal:host-gateway \
      "$IMAGE"
    echo "$ref @ $sha $(date -u +%FT%TZ)" > "$REF_FILE"
    echo "=== staging live: $ref ($sha) on :8081 ==="
}

case "${1:-status}" in
    pin)        deploy_ref origin/main ;;
    switch)     [ -n "${2:-}" ] || { echo "usage: staging.sh switch <branch>"; exit 1; }; deploy_ref "$2" ;;
    autopin)
        git -C "$SRC" fetch origin -q
        main_sha=$(git -C "$SRC" rev-parse origin/main)
        cur_sha=$(git -C "$SRC" rev-parse HEAD 2>/dev/null || echo none)
        # Only auto-track when staging is on the main track (manual branch
        # test-drives are left alone until you `staging.sh pin` again).
        if grep -q "^origin/main" "$REF_FILE" 2>/dev/null && [ "$main_sha" != "$cur_sha" ]; then
            deploy_ref origin/main
        else
            echo "staging autopin: no change (deployed: $(deployed_ref))"
        fi
        ;;
    status)
        echo "deployed: $(deployed_ref)"
        git -C "$SRC" fetch origin -q
        echo "origin/main: $(git -C "$SRC" rev-parse --short=12 origin/main)"
        docker ps --filter name=carbon-panel-staging --format '{{.Names}} {{.Status}}'
        ;;
    logs)       docker logs --tail "${2:-100}" carbon-panel-staging ;;
    *)          echo "usage: staging.sh {pin|switch <branch>|autopin|status|logs}"; exit 1 ;;
esac
