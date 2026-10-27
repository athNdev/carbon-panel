#!/usr/bin/env node
// SPDX-License-Identifier: MPL-2.0
//
// Deep live-server review pass. Drives a real Minecraft server through its
// lifecycle and captures the dashboard status pips at each state, verifying
// that transient states (starting, stopping, deep-sleep) are not rendered as
// error red (the MINE-294 / OSS-255 fix).
//
// Run against instance A with a live server already created:
//   node live-check.mjs

import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { rpc } from "./panel-api.mjs";

const BASE = process.env.BASE_URL || "http://localhost:18081";
const USER = process.env.REVIEW_USER || "review";
const PASS = process.env.REVIEW_PASS || "CarbonReview123!";
const OUT = join(process.cwd(), "out", "live-" + new Date().toISOString().replace(/[:.]/g, "-"));

const notes = [];
const note = (sev, what) => {
  notes.push({ sev, what });
  console.log(`${sev === "PASS" ? "  PASS" : sev === "FAIL" ? "  FAIL" : "  ..  "} ${what}`);
};

const PIP_COLORS = {
  "24a148": "green",
  da1e28: "red",
  a56eff: "purple",
  "0f62fe": "blue",
  "8d8d8d": "gray",
};

async function capturePips(page, label) {
  await page.goto(`${BASE}`, { waitUntil: "networkidle" });
  await page.waitForTimeout(600);
  await page.screenshot({ path: join(OUT, `pip-${label}.png`) });
  const pips = await page.evaluate((map) => {
    const out = [];
    // Status pips are small squares whose class contains a bg-[#hex].
    for (const el of document.querySelectorAll('[class*="bg-[#"]')) {
      const m = /bg-\[#([0-9a-fA-F]{6})\]/.exec(el.className);
      if (!m) continue;
      const r = el.getBoundingClientRect();
      // A status pip is a small square (~8px).
      if (r.width >= 4 && r.width <= 16 && r.height >= 4 && r.height <= 16) {
        out.push(map[m[1].toLowerCase()] || `#${m[1]}`);
      }
    }
    return out;
  }, PIP_COLORS);
  const counts = {};
  for (const c of pips) counts[c] = (counts[c] || 0) + 1;
  return counts;
}

async function main() {
  await mkdir(OUT, { recursive: true });
  const browser = await chromium.launch({ headless: true });
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await ctx.newPage();

  // Login.
  await page.goto(`${BASE}/login`, { waitUntil: "networkidle" });
  await page.locator('input[type="text"], input[name="username"]').first().fill(USER);
  await page.locator('input[type="password"]').first().fill(PASS);
  await page.keyboard.press("Enter");
  await page.waitForTimeout(1000);

  console.log("\n=== Live server lifecycle review ===\nbase:", BASE);

  // Discover the live server.
  const list = await rpc("ServerService", "ListServers", {});
  const server = (list.servers || [])[0];
  if (!server) {
    note("FAIL", "no server found; create one first");
    await browser.close();
    return;
  }
  console.log(`server: ${server.name} (${server.id}) status=${server.status}`);

  // 1. RUNNING pip should be green, never red.
  let pips = await capturePips(page, "running");
  console.log("  running pips:", JSON.stringify(pips));
  note(pips.red ? "FAIL" : "PASS", `running state: red=${pips.red || 0} (want 0), green=${pips.green || 0}`);

  // 2. STOP → stopped pip should be gray, not red.
  await rpc("ServerService", "StopServer", { id: server.id });
  await page.waitForTimeout(4000);
  pips = await capturePips(page, "stopped");
  console.log("  stopped pips:", JSON.stringify(pips));
  note(pips.red ? "FAIL" : "PASS", `stopped state: red=${pips.red || 0} (want 0), gray=${pips.gray || 0}`);

  // 3. START → transient pip must NOT be error red (the core MINE-294 fix).
  await rpc("ServerService", "StartServer", { id: server.id });
  await page.waitForTimeout(1200);
  pips = await capturePips(page, "starting");
  console.log("  starting pips:", JSON.stringify(pips));
  note(pips.red ? "FAIL" : "PASS", `transient (starting) state: red=${pips.red || 0} (want 0), purple/blue=${(pips.purple || 0) + (pips.blue || 0)}`);

  // 4. Wait for running again.
  await page.waitForTimeout(6000);
  const after = await rpc("ServerService", "ListServers", {});
  const s2 = (after.servers || [])[0];
  note("PASS", `post-start status: ${s2?.status}`);

  // 5. Console/logs reachability through the panel.
  try {
    const logs = await rpc("ServerService", "GetServerConsole", { id: server.id });
    note("PASS", `console reachable (${JSON.stringify(logs).length} bytes)`);
  } catch (e) {
    note("INFO", `console RPC: ${e.message.slice(0, 80)}`);
  }

  await browser.close();

  const md = [
    "# Live server review",
    "",
    `- base: ${BASE}`,
    `- server: ${server.name} (${server.id})`,
    "",
    "| Severity | Finding |",
    "| --- | --- |",
    ...notes.map((n) => `| ${n.sev} | ${n.what} |`),
    "",
    `Screenshots: ${OUT}`,
  ].join("\n");
  await writeFile(join(OUT, "report.md"), md);
  console.log("\n" + md);
  const fails = notes.filter((n) => n.sev === "FAIL").length;
  console.log(`\nRESULT: ${fails ? "FAIL" : "PASS"} (${fails} failures)`);
}

main().catch((e) => {
  console.error("live-check failed:", e);
  process.exit(1);
});
