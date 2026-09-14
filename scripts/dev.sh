#!/bin/bash
# ==============================================================================
# Carbon Panel Local Development Server Launcher (Bash)
# Launches Air (backend live reload) and Bun (frontend Vite HMR) concurrently.
# ==============================================================================

set -e

echo -e "\033[36m=================================================="
echo -e "   Carbon Panel Live Development Environment"
echo -e "=================================================="
echo -e "  Frontend (Vite HMR):  http://localhost:5174"
echo -e "  Backend API / RPC:    http://localhost:8080"
echo -e "==================================================\033[0m\n"

# Ensure data and tmp directories exist
mkdir -p data tmp

# Check for air binary in PATH, GOPATH/bin, or GOBIN
AIR_CMD="air"
if ! command -v air &> /dev/null; then
    GOPATH="${GOPATH:-$HOME/go}"
    if [ -f "$GOPATH/bin/air" ]; then
        AIR_CMD="$GOPATH/bin/air"
    else
        echo -e "\033[33mAir hot-reloader not found in PATH. Install with: go install github.com/air-verse/air@latest\033[0m"
        echo -e "\033[33mFalling back to 'go run cmd/carbon-panel/main.go'...\033[0m"
        AIR_CMD="go run cmd/carbon-panel/main.go"
    fi
fi

# Cleanup on exit
trap 'echo -e "\n\033[33mStopping dev servers...\033[0m"; kill $(jobs -p) 2>/dev/null || true; wait; exit' INT TERM EXIT

# Start backend
$AIR_CMD &
BACKEND_PID=$!

# Start frontend
cd web/carbon-panel && bun run dev &
FRONTEND_PID=$!

wait $BACKEND_PID $FRONTEND_PID
