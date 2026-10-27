#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0
#
# Local manual-review harness for Carbon Panel.
#
# Runs one or two throwaway panels from a git ref inside Docker (WSL), with
# isolated data directories, auto-created admin accounts, stable ports and a
# shared Docker daemon so cross-installation ownership can be reviewed.
#
# Usage (from Windows):
#   wsl.exe -e bash scripts/manual-review/review.sh up [--instances 2] [--ref HEAD]
#   wsl.exe -e bash scripts/manual-review/review.sh status
#   wsl.exe -e bash scripts/manual-review/review.sh down
#
# Safe by design: state lives outside the repository in
#   $HOME/.cache/carbon-review/<sha>/
# and `down` keeps data unless `--purge` is passed explicitly.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CACHE_ROOT="${XDG_CACHE_HOME:-$HOME/.cache}/carbon-review"

BASE_PORT_A=18081
BASE_PORT_B=18082
ADMIN_USER="review"
ADMIN_EMAIL="review@carbon.invalid"
ADMIN_PASSWORD="CarbonReview123!"

usage() {
  cat <<'EOF'
Usage: review.sh <command> [options]

Commands:
  up        Build (if needed) and start the review instance(s)
  status    Show instance URLs, health and data directories
  logs      Follow panel logs (a|b)
  down      Stop instances (keeps data)
  reset     Stop instances and delete their data (explicit, destructive)
  urls      Print review URLs only

Options for `up`:
  --ref <git-ref>     Commit/branch to run (default: HEAD)
  --instances <1|2>   Start one or two panels on one Docker daemon (default: 2)
  --no-build          Reuse an existing image for this ref
  --recreate          Recreate containers even if they already run

Review credentials (local-only, not a secret):
  user: review
  pass: CarbonReview123!
EOF
}

log() { printf '\033[36m[review]\033[0m %s\n' "$*"; }
warn() { printf '\033[33m[review]\033[0m %s\n' "$*" >&2; }
die() { printf '\033[31m[review] %s\033[0m\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"; }

resolve_ref() {
  git -C "$REPO_ROOT" rev-parse --verify "$1^{commit}" 2>/dev/null ||
    die "unknown ref: $1"
}

state_dir_for() { echo "$CACHE_ROOT/${1:0:12}"; }
compose_file() { echo "$1/compose.yaml"; }
project_name() { echo "carbon-review-${1:0:12}"; }

current_sha() {
  if [ -f "$CACHE_ROOT/current" ]; then cat "$CACHE_ROOT/current"; fi
}

ensure_worktree() {
  local sha="$1" dir="$2"
  if [ -e "$dir/src/.git" ]; then
    local existing
    existing="$(git -C "$dir/src" rev-parse HEAD 2>/dev/null || true)"
    [ "$existing" = "$sha" ] && return 0
    git -C "$REPO_ROOT" worktree remove --force "$dir/src" >/dev/null 2>&1 || true
    rm -rf "$dir/src"
  fi
  mkdir -p "$dir"
  git -C "$REPO_ROOT" worktree add --detach --force "$dir/src" "$sha" >/dev/null
}

image_exists() { docker image inspect "$1" >/dev/null 2>&1; }

build_image() {
  local sha="$1" dir="$2"
  local tag="carbon-panel:review-${sha:0:12}"
  if image_exists "$tag"; then
    log "image $tag already present"
    return 0
  fi
  log "building $tag (proto + frontend + backend; first run can take several minutes)"
  docker build \
    --build-arg APP_VERSION="review-${sha:0:12}" \
    -f "$dir/src/docker/Dockerfile.carbon-panel" \
    -t "$tag" \
    "$dir/src"
}

port_free() {
  ! ss -ltn 2>/dev/null | awk '{print $4}' | grep -qE "[:.]$1$"
}

write_compose() {
  local sha="$1" dir="$2" tag="carbon-panel:review-${1:0:12}" instances="$3"
  local project base
  project="$(project_name "$sha")"
  base="$CACHE_ROOT/${sha:0:12}"

  health() {
    cat <<'EOF'
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/healthz"]
      interval: 3s
      timeout: 3s
      retries: 40
      start_period: 10s
EOF
  }

  {
    printf 'name: %s\n\nservices:\n' "$project"
    printf '  panel-a:\n'
    printf '    image: %s\n' "$tag"
    printf '    container_name: %s-a\n' "$project"
    printf '    restart: unless-stopped\n'
    printf '    ports:\n      - "%s:8080"\n' "$BASE_PORT_A"
    printf '    volumes:\n'
    printf '      - /var/run/docker.sock:/var/run/docker.sock\n'
    printf '      - %s/a/data:/app/data\n' "$base"
    printf '      - %s/a/backups:/app/backups\n' "$base"
    printf '      - %s/a/tmp:/app/tmp\n' "$base"
    printf '    environment:\n'
    printf '      CARBONPANEL_DATA_DIR: /app/data\n'
    printf '      CARBONPANEL_HOST_DATA_PATH: %s/a/data\n' "$base"
    printf '      TZ: UTC\n'
    printf '    extra_hosts:\n      - "host.docker.internal:host-gateway"\n'
    health

    if [ "$instances" = "2" ]; then
      printf '\n  panel-b:\n'
      printf '    image: %s\n' "$tag"
      printf '    container_name: %s-b\n' "$project"
      printf '    restart: unless-stopped\n'
      printf '    ports:\n      - "%s:8080"\n' "$BASE_PORT_B"
      printf '    volumes:\n'
      printf '      - /var/run/docker.sock:/var/run/docker.sock\n'
      printf '      - %s/b/data:/app/data\n' "$base"
      printf '      - %s/b/backups:/app/backups\n' "$base"
      printf '      - %s/b/tmp:/app/tmp\n' "$base"
      printf '    environment:\n'
      printf '      CARBONPANEL_DATA_DIR: /app/data\n'
      printf '      CARBONPANEL_HOST_DATA_PATH: %s/b/data\n' "$base"
      printf '      TZ: UTC\n'
      printf '    extra_hosts:\n      - "host.docker.internal:host-gateway"\n'
      health
    fi
  } > "$(compose_file "$dir")"
}

compose() {
  local sha="$1" dir="$2"; shift 2
  docker compose -f "$(compose_file "$dir")" -p "$(project_name "$sha")" "$@"
}

wait_healthy() {
  local container="$1" deadline=$(( $(date +%s) + 240 ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    local status
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container" 2>/dev/null || echo missing)"
    [ "$status" = "healthy" ] && return 0
    sleep 2
  done
  return 1
}

bootstrap_admin() {
  local container="$1" port="$2"
  local payload response
  payload="{\"username\":\"$ADMIN_USER\",\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}"
  response="$(docker exec "$container" wget -qO- \
    --header="Content-Type: application/json" \
    --post-data="$payload" \
    "http://127.0.0.1:8080/carbonpanel.v1.AuthService/Register" 2>/dev/null || true)"
  if [ -n "$response" ]; then
    log "admin $ADMIN_USER bootstrapped on :$port"
  else
    warn "admin bootstrap returned no body on :$port (account may already exist)"
  fi
}

cmd_up() {
  local ref="HEAD" instances="2" build=1 recreate=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --ref) ref="$2"; shift 2 ;;
      --instances) instances="$2"; shift 2 ;;
      --no-build) build=0; shift ;;
      --recreate) recreate="--force-recreate"; shift ;;
      *) die "unknown option: $1" ;;
    esac
  done
  [ "$instances" = "1" ] || [ "$instances" = "2" ] || die "--instances must be 1 or 2"

  need docker; need git
  docker compose version >/dev/null 2>&1 || die "docker compose plugin required"
  docker info >/dev/null 2>&1 || die "Docker daemon not reachable inside WSL"

  local sha dir
  sha="$(resolve_ref "$ref")"
  dir="$(state_dir_for "$sha")"; mkdir -p "$dir"

  port_free "$BASE_PORT_A" || die "port $BASE_PORT_A is already in use"
  if [ "$instances" = "2" ]; then
    port_free "$BASE_PORT_B" || die "port $BASE_PORT_B is already in use"
  fi

  ensure_worktree "$sha" "$dir"
  [ "$build" = "1" ] && build_image "$sha" "$dir"

  write_compose "$sha" "$dir" "$instances"
  echo "$sha" > "$CACHE_ROOT/current"

  log "starting $(project_name "$sha") from ${sha:0:12}"
  compose "$sha" "$dir" up -d $recreate

  local suffix container
  for suffix in a b; do
    [ "$suffix" = "b" ] && [ "$instances" = "1" ] && continue
    container="$(project_name "$sha")-$suffix"
    if wait_healthy "$container"; then
      if [ "$suffix" = "b" ]; then
        bootstrap_admin "$container" "$BASE_PORT_B"
      else
        bootstrap_admin "$container" "$BASE_PORT_A"
      fi
    else
      warn "$container did not become healthy; inspect: docker logs $container"
    fi
  done

  echo
  log "review instances ready:"
  printf '  A  http://localhost:%s   (container %s-a)\n' "$BASE_PORT_A" "$(project_name "$sha")"
  [ "$instances" = "2" ] && printf '  B  http://localhost:%s   (container %s-b)\n' "$BASE_PORT_B" "$(project_name "$sha")"
  printf '  login: %s / %s\n' "$ADMIN_USER" "$ADMIN_PASSWORD"
  printf '  data:  %s/{a,b}\n' "$dir"
  printf '  image: carbon-panel:review-%s\n' "${sha:0:12}"
}

cmd_status() {
  local sha dir container port health
  sha="$(current_sha)"; [ -n "$sha" ] || die "no review instance recorded; run: review.sh up"
  dir="$(state_dir_for "$sha")"
  printf 'ref      %s\n' "$sha"
  printf 'state    %s\n' "$dir"
  printf 'image    carbon-panel:review-%s\n\n' "${sha:0:12}"
  printf '%-4s %-38s %-10s %s\n' "inst" "url" "health" "container"
  for suffix in a b; do
    container="$(project_name "$sha")-$suffix"
    docker inspect "$container" >/dev/null 2>&1 || continue
    port=$BASE_PORT_A; [ "$suffix" = "b" ] && port=$BASE_PORT_B
    health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container")"
    printf '%-4s %-38s %-10s %s\n' "$suffix" "http://localhost:$port" "$health" "$container"
  done
  printf '\nlogin    %s / %s\n' "$ADMIN_USER" "$ADMIN_PASSWORD"
}

cmd_logs() {
  local which="${1:-a}" sha dir
  [ "$which" = "a" ] || [ "$which" = "b" ] || die "logs <a|b>"
  sha="$(current_sha)"; [ -n "$sha" ] || die "no review instance recorded"
  dir="$(state_dir_for "$sha")"
  docker logs -f --tail=200 "$(project_name "$sha")-$which"
}

cmd_down() {
  local purge="" sha dir
  [ "${1:-}" = "--purge" ] && purge="--purge"
  sha="$(current_sha)"; [ -n "$sha" ] || die "no review instance recorded"
  dir="$(state_dir_for "$sha")"
  compose "$sha" "$dir" down --remove-orphans || true
  log "instances stopped; data kept at $dir"
  if [ "$purge" = "--purge" ]; then
    rm -rf "$dir" "$CACHE_ROOT/current"
    log "review data purged"
  fi
}

cmd_reset() { cmd_down --purge; }

cmd_urls() {
  local sha
  sha="$(current_sha)"; [ -n "$sha" ] || die "no review instance recorded"
  echo "http://localhost:$BASE_PORT_A"
  docker inspect "$(project_name "$sha")-b" >/dev/null 2>&1 && echo "http://localhost:$BASE_PORT_B"
}

command="${1:-}"; shift || true
case "$command" in
  up) cmd_up "$@" ;;
  status) cmd_status ;;
  logs) cmd_logs "$@" ;;
  down) cmd_down "${1:-}" ;;
  reset) cmd_reset ;;
  urls) cmd_urls ;;
  ""|-h|--help|help) usage ;;
  *) usage; exit 2 ;;
esac
