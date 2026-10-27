#!/usr/bin/env node
// SPDX-License-Identifier: MPL-2.0
//
// Minimal Carbon Panel RPC helper for the review harness. Logs in with the
// review credentials and exposes the ConnectRPC endpoints so a reviewer (or a
// test) can drive the backend directly: register nodes, create servers, and
// inspect state without clicking through the wizard.
//
// Usage:
//   node panel-api.mjs NodeService ListNodes '{}'
//   node panel-api.mjs NodeService CreateNode '{"name":"local","host":"unix:///var/run/docker.sock","enabled":true}'
//   node panel-api.mjs ServerService CreateServer '{"name":"mc","modLoader":"MOD_LOADER_VANILLA","mcVersion":"1.21.1","port":25565,"maxPlayers":10,"memory":2048,"startImmediately":true}'
//
// Set BASE_URL to target a different instance (default http://localhost:18081).

import { readFileSync } from "node:fs";

const BASE = process.env.BASE_URL || "http://localhost:18081";
const USER = process.env.REVIEW_USER || "review";
const PASS = process.env.REVIEW_PASS || "CarbonReview123!";

let cachedToken = null;

async function login() {
  if (cachedToken) return cachedToken;
  const res = await fetch(`${BASE}/carbonpanel.v1.AuthService/Login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username: USER, password: PASS }),
  });
  if (!res.ok) throw new Error(`login failed: ${res.status} ${await res.text()}`);
  const data = await res.json();
  cachedToken = data.token;
  return cachedToken;
}

export async function rpc(service, method, body) {
  const token = await login();
  const res = await fetch(`${BASE}/carbonpanel.v1.${service}/${method}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify(body ?? {}),
  });
  const text = await res.text();
  let json;
  try {
    json = JSON.parse(text);
  } catch {
    json = text;
  }
  if (!res.ok) {
    const err = new Error(`${service}/${method} -> ${res.status}: ${text.slice(0, 400)}`);
    err.status = res.status;
    err.body = json;
    throw err;
  }
  return json;
}

// CLI entry point.
// CLI entry point (only when run directly, not when imported).
import { pathToFileURL } from "node:url";
const isMain = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href;
if (isMain) {
  const [service, method, bodyArg] = process.argv.slice(2);
  let body = {};
  if (bodyArg) {
    // "@path" reads the JSON body from a file, avoiding shell-quoting issues.
    body = bodyArg.startsWith("@")
      ? JSON.parse(readFileSync(bodyArg.slice(1), "utf8"))
      : JSON.parse(bodyArg);
  }
  rpc(service, method, body)
    .then((out) => console.log(JSON.stringify(out, null, 2)))
    .catch((e) => {
      console.error(e.message);
      process.exit(1);
    });
} else if (isMain) {
  console.error("usage: panel-api.mjs <Service> <Method> '<json>'");
  process.exit(2);
}
