<#
.SYNOPSIS
    MineServer Local Development Server Launcher (PowerShell)
.DESCRIPTION
    Launches Air (Go backend hot reloading) and Bun (SvelteKit Vite HMR) concurrently.
#>

$ErrorActionPreference = "Stop"

Write-Host "==================================================" -ForegroundColor Cyan
Write-Host "   MineServer Live Development Environment" -ForegroundColor Cyan
Write-Host "==================================================" -ForegroundColor Cyan
Write-Host "  Frontend (Vite HMR):  http://localhost:5174" -ForegroundColor Green
Write-Host "  Backend API / RPC:    http://localhost:8080" -ForegroundColor Green
Write-Host "==================================================`n" -ForegroundColor Cyan

New-Item -ItemType Directory -Force -Path "data", "tmp" | Out-Null

# Locate Air binary
$airCmd = "air"
$hasAir = (Get-Command "air" -ErrorAction SilentlyContinue)
if (-not $hasAir) {
    $gopath = if ($env:GOPATH) { $env:GOPATH } else { Join-Path $env:USERPROFILE "go" }
    $gopathAir = Join-Path $gopath "bin\air.exe"
    if (Test-Path $gopathAir) {
        $airCmd = $gopathAir
        $hasAir = $true
    }
}

$frontendDir = Join-Path $PSScriptRoot "..\web\mineserver"

if ($hasAir) {
    Write-Host "[dev] Starting backend with Air (hot reloading)..." -ForegroundColor Magenta
    $backendProc = Start-Process -FilePath $airCmd -NoNewWindow -PassThru
} else {
    Write-Host "[dev] Air not found. Starting with 'go run cmd/mineserver/main.go'..." -ForegroundColor Yellow
    Write-Host "[dev] (Tip: install air with 'go install github.com/air-verse/air@latest' for live reloading)" -ForegroundColor Gray
    $backendProc = Start-Process -FilePath "go" -ArgumentList "run", "cmd/mineserver/main.go" -NoNewWindow -PassThru
}

Write-Host "[dev] Starting frontend with Bun (Vite HMR)..." -ForegroundColor Cyan
$frontendProc = Start-Process -FilePath "bun" -ArgumentList "run", "dev" -WorkingDirectory $frontendDir -NoNewWindow -PassThru

try {
    while (-not $backendProc.HasExited -or -not $frontendProc.HasExited) {
        Start-Sleep -Seconds 1
    }
} finally {
    Write-Host "`n[dev] Shutting down dev servers..." -ForegroundColor Yellow
    if ($backendProc -and -not $backendProc.HasExited) {
        Stop-Process -Id $backendProc.Id -Force -ErrorAction SilentlyContinue
    }
    if ($frontendProc -and -not $frontendProc.HasExited) {
        Stop-Process -Id $frontendProc.Id -Force -ErrorAction SilentlyContinue
    }
}
