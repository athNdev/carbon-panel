#!/usr/bin/env node
// SPDX-License-Identifier: MPL-2.0
//
// Minecraft ecosystem boot matrix: creates, boots and verifies one server per
// loader/version combination against a running review instance, then cleans
// up. This is the live "deep and wide" pass for loader x version support:
// every combo must provision, reach RUNNING, and answer the console.
//
// Usage:
//   node matrix-check.mjs                            # default representative set
//   MATRIX=fabric:1.21.1,forge:1.20.4 node matrix-check.mjs
//   BOOT_TIMEOUT_MS=420000 node matrix-check.mjs     # per-combo boot budget
//
// Env: BASE_URL, REVIEW_USER, REVIEW_PASS as in the other harnesses.

import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { rpc } from "./panel-api.mjs";

const BASE = process.env.BASE_URL || "http://localhost:18081";
const BOOT_TIMEOUT_MS = Number(process.env.BOOT_TIMEOUT_MS || 420000);
const POLL_MS = 5000;
const OUT = join(process.cwd(), "out", `matrix-${new Date().toISOString().replace(/[:.]/g, "-")}`);

// Representative set: every major loader family plus two vanilla lines.
// Forge is slowest (installer boot) and available via MATRIX.
const DEFAULT_MATRIX = [
  ["vanilla", "1.21.1"],
  ["fabric", "1.21.1"],
  ["fabric", "1.20.4"],
  ["quilt", "1.21.1"],
  ["neoforge", "1.20.4"],
  ["paper", "1.21.1"],
];

const LOADER_ENUM = {
  vanilla: "MOD_LOADER_VANILLA",
  fabric: "MOD_LOADER_FABRIC",
  forge: "MOD_LOADER_FORGE",
  neoforge: "MOD_LOADER_NEOFORGE",
  quilt: "MOD_LOADER_QUILT",
  paper: "MOD_LOADER_PAPER",
};

function parseMatrix() {
  if (!process.env.MATRIX) return DEFAULT_MATRIX;
  return process.env.MATRIX.split(",")
    .map((s) => s.trim())
    .filter(Boolean)
    .map((s) => {
      const [loader, version] = s.split(":");
      return [loader.trim(), version.trim()];
    });
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function statusOf(id) {
  const res = await rpc("ServerService", "ListServers", {});
  const server = (res.servers || []).find((s) => s.id === id);
  return server?.status || "SERVER_STATUS_UNKNOWN";
}

async function consoleAnswers(id) {
  // Boot proof: the `list` command answers on every server flavor only once the
  // server tick loop is up. Retry while the console buffers early output.
  // An "Unknown command" reply is not proof and never passes.
  for (let attempt = 0; attempt < 6; attempt++) {
    try {
      const res = await rpc("ServerService", "SendCommand", { id, command: "list" });
      const out = (res.output || "").trim();
      if (out && !/unknown command|incomplete command/i.test(out)) return out;
    } catch {
      // console not attached yet
    }
    await sleep(5000);
  }
  return "";
}

async function destroyServer(id) {
  try {
    await rpc("ServerService", "StopServer", { id });
    await sleep(3000);
  } catch {
    // already stopped
  }
  try {
    await rpc("ServerService", "DeleteServer", { id });
  } catch {
    // best-effort cleanup
  }
}

async function runCombo([loader, version], index, total) {
  const label = `${loader} ${version}`;
  console.log(`\n[${index + 1}/${total}] ${label}`);
  const modLoader = LOADER_ENUM[loader];
  if (!modLoader) {
    return { label, ok: false, note: `unknown loader "${loader}"` };
  }

  let id;
  try {
    const portRes = await rpc("ServerService", "GetNextAvailablePort", {});
    const createRes = await rpc("ServerService", "CreateServer", {
      name: `mx-${loader}-${version.replace(/\./g, "_")}`.slice(0, 32),
      modLoader,
      mcVersion: version,
      port: portRes.port,
      maxPlayers: 5,
      memory: 3072,
      startImmediately: false,
    });
    id = createRes.server?.id;
    if (!id) return { label, ok: false, note: "create returned no id" };

    // Creation is asynchronous and holds the lifecycle transition across the
    // image pull, so wait it out before starting.
    const createT0 = Date.now();
    while (Date.now() - createT0 < BOOT_TIMEOUT_MS) {
      const s = await statusOf(id);
      if (s !== "SERVER_STATUS_CREATING" && s !== "SERVER_STATUS_UNKNOWN") break;
      await sleep(POLL_MS);
    }

    // A fresh create may hold the lifecycle transition for a moment.
    let started = false;
    for (let attempt = 0; attempt < 3 && !started; attempt++) {
      try {
        await rpc("ServerService", "StartServer", { id });
        started = true;
      } catch (e) {
        if (attempt === 2) throw e;
        await sleep(5000);
      }
    }

    const t0 = Date.now();
    let status = "SERVER_STATUS_STARTING";
    while (Date.now() - t0 < BOOT_TIMEOUT_MS) {
      status = await statusOf(id);
      if (status === "SERVER_STATUS_RUNNING" || status === "SERVER_STATUS_ERROR") break;
      await sleep(POLL_MS);
    }
    const bootSec = Math.round((Date.now() - t0) / 1000);

    if (status !== "SERVER_STATUS_RUNNING") {
      return { label, ok: false, note: `stuck in ${status} after ${bootSec}s`, id };
    }

    const output = await consoleAnswers(id);
    if (!output) {
      return { label, ok: false, note: `RUNNING in ${bootSec}s but console silent`, id };
    }
    return { label, ok: true, note: `RUNNING in ${bootSec}s; console: ${output.split("\n")[0].slice(0, 80)}`, id };
  } catch (e) {
    return { label, ok: false, note: String(e.message || e).slice(0, 140), id };
  } finally {
    if (id) await destroyServer(id);
  }
}

async function main() {
  mkdirSync(OUT, { recursive: true });
  const matrix = parseMatrix();
  console.log(`matrix: ${matrix.map(([l, v]) => `${l}:${v}`).join(", ")} (boot budget ${BOOT_TIMEOUT_MS / 1000}s each)`);

  const results = [];
  for (let i = 0; i < matrix.length; i++) {
    results.push(await runCombo(matrix[i], i, matrix.length));
  }

  console.log("\n=== boot matrix ===");
  for (const r of results) {
    console.log(`${r.ok ? "PASS" : "FAIL"}  ${r.label.padEnd(18)} ${r.note}`);
  }
  const failed = results.filter((r) => !r.ok);
  const report = [
    "# Minecraft ecosystem boot matrix",
    "",
    `- base: ${BASE}`,
    `- at: ${new Date().toISOString()}`,
    "",
    "| Combo | Result | Evidence |",
    "| --- | --- | --- |",
    ...results.map((r) => `| ${r.label} | ${r.ok ? "PASS" : "FAIL"} | ${r.note} |`),
    "",
    `## Counts: ${results.length - failed.length} pass, ${failed.length} fail`,
  ].join("\n");
  writeFileSync(join(OUT, "report.md"), report);
  console.log(`\n${results.length - failed.length} pass, ${failed.length} fail — report: ${OUT}`);
  if (failed.length) process.exitCode = 1;
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
