import { spawn, type ChildProcess } from "node:child_process";
import { existsSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";

const isWindows = process.platform === "win32";

console.log("\x1b[36m%s\x1b[0m", "==================================================");
console.log("\x1b[36m%s\x1b[0m", "   Carbon Panel Live Development Environment");
console.log("\x1b[36m%s\x1b[0m", "==================================================");
console.log("\x1b[32m%s\x1b[0m", "  Frontend (Vite HMR):  http://localhost:5174");
console.log("\x1b[32m%s\x1b[0m", "  Backend API / RPC:    http://localhost:8080");
console.log("\x1b[36m%s\x1b[0m", "==================================================\n");

function findAirBinary(): string {
  // 1. In PATH
  const airName = isWindows ? "air.exe" : "air";
  
  // 2. Check GOPATH/bin
  const gopath = process.env.GOPATH || join(homedir(), "go");
  const gopathAir = join(gopath, "bin", airName);
  if (existsSync(gopathAir)) {
    return gopathAir;
  }

  // 3. Check GOBIN
  if (process.env.GOBIN) {
    const gobinAir = join(process.env.GOBIN, airName);
    if (existsSync(gobinAir)) {
      return gobinAir;
    }
  }

  return "air";
}

const activeProcesses: ChildProcess[] = [];

function cleanup() {
  console.log("\n\x1b[33m%s\x1b[0m", "Shutting down dev servers...");
  for (const proc of activeProcesses) {
    if (proc && !proc.killed && proc.pid) {
      try {
        if (isWindows) {
          spawn("taskkill", ["/pid", proc.pid.toString(), "/f", "/t"], { stdio: "ignore" });
        } else {
          proc.kill("SIGTERM");
        }
      } catch {
        /* ignore */
      }
    }
  }
  process.exit(0);
}

process.on("SIGINT", cleanup);
process.on("SIGTERM", cleanup);
process.on("exit", cleanup);

// 1. Start Backend with Air (live hot reload)
const airCmd = findAirBinary();
let backendProc: ChildProcess;

try {
  backendProc = spawn(airCmd, [], {
    stdio: "inherit",
    shell: isWindows,
    cwd: process.cwd(),
    env: { ...process.env, FORCE_COLOR: "true" }
  });

  backendProc.on("error", (err) => {
    console.warn("\x1b[33m%s\x1b[0m", `[backend] Air not found or error (${err.message}). Falling back to 'go run cmd/carbon-panel/main.go'...`);
    const fallbackProc = spawn("go", ["run", "cmd/carbon-panel/main.go"], {
      stdio: "inherit",
      shell: isWindows,
      cwd: process.cwd()
    });
    activeProcesses.push(fallbackProc);
  });

  activeProcesses.push(backendProc);
} catch (e) {
  console.warn("\x1b[33m%s\x1b[0m", `[backend] Starting with 'go run cmd/carbon-panel/main.go'...`);
  const fallbackProc = spawn("go", ["run", "cmd/carbon-panel/main.go"], {
    stdio: "inherit",
    shell: isWindows,
    cwd: process.cwd()
  });
  activeProcesses.push(fallbackProc);
}

// 2. Start Frontend with Bun (Vite HMR)
const frontendDir = join(process.cwd(), "web", "carbon-panel");
const frontendProc = spawn("bun", ["run", "dev"], {
  stdio: "inherit",
  shell: isWindows,
  cwd: frontendDir,
  env: { ...process.env, FORCE_COLOR: "true" }
});

frontendProc.on("error", (err) => {
  console.error("\x1b[31m%s\x1b[0m", `[frontend] Failed to start frontend dev server: ${err.message}`);
});

activeProcesses.push(frontendProc);
