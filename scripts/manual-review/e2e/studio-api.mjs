#!/usr/bin/env node
// SPDX-License-Identifier: MPL-2.0
//
// Minimal Carbon Panel packwiz (Modpack Studio) REST helper for the review
// harness. Logs in with the review credentials and calls the studio HTTP API.
//
// Usage:
//   node studio-api.mjs GET  packs
//   node studio-api.mjs POST packs '{"id":"demo","name":"Demo","mc_version":"1.21.1","mod_loader":"fabric"}'
//   node studio-api.mjs POST packs/demo/datapacks '{"datapack_id":"QWfaJXEc","game_version":"1.21.1"}'
//   node studio-api.mjs GET  packs/demo/files
//
// The path is relative to /api/v1/packwiz/. Set BASE_URL to target a
// different instance (default http://localhost:18081).

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
  cachedToken = (await res.json()).token;
  return cachedToken;
}

export async function studio(method, path, body) {
  const token = await login();
  const res = await fetch(`${BASE}/api/v1/packwiz/${path}`, {
    method,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token}`,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  let json;
  try {
    json = JSON.parse(text);
  } catch {
    json = text;
  }
  if (!res.ok) {
    throw new Error(`studio ${method} /${path} -> ${res.status}: ${text.slice(0, 400)}`);
  }
  return json;
}

import { pathToFileURL } from "node:url";
const isMain = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href;
if (isMain) {
  const [method, path, bodyArg] = process.argv.slice(2);
  if (!method || !path) {
    console.error("usage: studio-api.mjs <METHOD> <path> ['<json>'|@file]");
    process.exit(2);
  }
  let body;
  if (bodyArg) {
    body = bodyArg.startsWith("@")
      ? JSON.parse(readFileSync(bodyArg.slice(1), "utf8"))
      : JSON.parse(bodyArg);
  }
  studio(method, path, body)
    .then((out) => console.log(JSON.stringify(out, null, 2)))
    .catch((e) => {
      console.error(e.message);
      process.exit(1);
    });
}
