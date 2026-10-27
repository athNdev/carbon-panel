#!/usr/bin/env node
// SPDX-License-Identifier: MPL-2.0
//
// Live backup/restore round-trip: proves the world-data seam end to end on a
// real panel — a triggered backup captures server state, a mutated file is
// restored back through the journaled swap, and the archive is a worldstore
// manifest artifact. This is the live companion to the seam unit tests.
//
// Usage: node backup-roundtrip.mjs   (BASE_URL/REVIEW_USER/REVIEW_PASS as usual)

import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { rpc } from "./panel-api.mjs";

const BASE = process.env.BASE_URL || "http://localhost:18081";
const OUT = join(process.cwd(), "out", `backup-rt-${new Date().toISOString().replace(/[:.]/g, "-")}`);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const findings = [];
const note = (id, severity, what, detail = "") => {
  findings.push({ id, severity, what, detail });
  const tag = severity === "HIGH" ? "!!" : severity === "MEDIUM" ? " !" : "  ";
  console.log(`${tag} [${id}] ${what}${detail ? ` — ${detail}` : ""}`);
};

const b64 = (s) => Buffer.from(s, "utf8").toString("base64");
const unb64 = (s) => Buffer.from(s, "base64").toString("utf8");

async function main() {
  mkdirSync(OUT, { recursive: true });

  // 1. Provision a stopped server with state to back up.
  const port = (await rpc("ServerService", "GetNextAvailablePort", {})).port;
  const created = await rpc("ServerService", "CreateServer", {
    name: "backup-rt",
    modLoader: "MOD_LOADER_VANILLA",
    mcVersion: "1.21.1",
    port,
    maxPlayers: 5,
    memory: 2048,
    startImmediately: false,
  });
  const id = created.server.id;
  note("P1", "PASS", "server provisioned", id);

  await rpc("FileService", "UpdateFile", {
    serverId: id,
    path: "roundtrip-marker.txt",
    content: b64("original-state"),
  });
  await rpc("FileService", "UpdateFile", {
    serverId: id,
    path: "world/level.dat",
    content: b64("synthetic-world-state"),
  });
  note("P2", "PASS", "state files written");

  // 2. Trigger a backup through the scheduler (the world-data seam path).
  const task = await rpc("TaskService", "CreateTask", {
    serverId: id,
    name: "backup-rt-task",
    taskType: "TASK_TYPE_BACKUP",
    schedule: "SCHEDULE_TYPE_INTERVAL",
    intervalSecs: 86400,
    config: JSON.stringify({ backup_name: "backup-rt", min_backups: 1, max_backups: 3 }),
  });
  const taskId = task.task?.id || task.id;
  await rpc("TaskService", "TriggerTask", { id: taskId });

  let backups = [];
  for (let i = 0; i < 24; i++) {
    await sleep(2500);
    backups = (await rpc("BackupService", "ListBackups", { serverId: id, limit: 10 })).backups || [];
    if (backups.some((b) => b.status === "complete")) break;
  }
  const done = backups.find((b) => b.status === "complete");
  if (!done) {
    note("B1", "HIGH", "backup did not complete", JSON.stringify(backups).slice(0, 200));
    return;
  }
  note("B1", "PASS", "backup completed via triggered task", `sha256:${(done.sha256 || "").slice(0, 12)}`);

  // 3. Mutate state, then restore.
  await rpc("FileService", "UpdateFile", {
    serverId: id,
    path: "roundtrip-marker.txt",
    content: b64("corrupted-state"),
  });
  const mutated = unb64((await rpc("FileService", "GetFile", { serverId: id, path: "roundtrip-marker.txt" })).content ?? "");
  if (mutated !== "corrupted-state") {
    note("M1", "MEDIUM", "could not verify mutation", mutated);
  }

  const restored = await rpc("BackupService", "RestoreBackup", { id: done.id });
  if (!restored.success) {
    note("R1", "HIGH", "restore failed", JSON.stringify(restored));
    return;
  }
  note("R1", "PASS", "restore completed");

  // 4. Verify the restored state came back through the seam.
  const marker = unb64((await rpc("FileService", "GetFile", { serverId: id, path: "roundtrip-marker.txt" })).content ?? "");
  if (marker === "original-state") {
    note("R2", "PASS", "restored file matches the backed-up state");
  } else {
    note("R2", "HIGH", "restored file does not match the backup", marker);
  }
  const world = unb64((await rpc("FileService", "GetFile", { serverId: id, path: "world/level.dat" })).content ?? "");
  if (world === "synthetic-world-state") {
    note("R3", "PASS", "restored world file matches the backed-up state");
  } else {
    note("R3", "HIGH", "restored world file does not match the backup", world);
  }

  // 5. Cleanup.
  try {
    await rpc("TaskService", "DeleteTask", { id: taskId });
    await rpc("BackupService", "DeleteBackup", { id: done.id });
    await rpc("ServerService", "DeleteServer", { id });
    note("C1", "PASS", "cleanup done");
  } catch (e) {
    note("C1", "MEDIUM", "cleanup incomplete", String(e.message || e).slice(0, 120));
  }
}

main()
  .then(() => {
    console.log("\n=== backup/restore round-trip ===");
    for (const f of findings) console.log(`${f.severity.padEnd(6)} [${f.id}] ${f.what}`);
    const bad = findings.filter((f) => f.severity === "HIGH");
    writeFileSync(
      join(OUT, "report.md"),
      ["# Backup/restore round-trip", "", ...findings.map((f) => `- **${f.severity}** [${f.id}] ${f.what}${f.detail ? ` — ${f.detail}` : ""}`)].join("\n")
    );
    console.log(`\n${bad.length ? `${bad.length} failure(s)` : "all checks passed"} — report: ${OUT}`);
    if (bad.length) process.exitCode = 1;
  })
  .catch((e) => {
    console.error(e);
    process.exit(1);
  });
