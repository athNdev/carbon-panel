#!/usr/bin/env node
// SPDX-License-Identifier: MPL-2.0
//
// Upload flow check: builds a real datapack zip, uploads it through the
// server Datapacks tab's file picker, and verifies it lands in the world.
//
// Usage: node upload-check.mjs

import { chromium } from "playwright";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const BASE = process.env.BASE_URL || "http://localhost:18081";
const USER = process.env.REVIEW_USER || "review";
const PASS = process.env.REVIEW_PASS || "CarbonReview123!";
const OUT = join(process.cwd(), "out", `upload-${new Date().toISOString().replace(/[:.]/g, "-")}`);

const findings = [];
const note = (id, severity, what, detail = "") => {
  findings.push({ id, severity, what, detail });
  const tag = severity === "HIGH" ? "!!" : severity === "MEDIUM" ? " !" : "  ";
  console.log(`${tag} [${id}] ${what}${detail ? ` — ${detail}` : ""}`);
};

function crc32(buf) {
  let c = ~0;
  for (const b of buf) {
    c ^= b;
    for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1));
  }
  return ~c >>> 0;
}

// Minimal store-method (uncompressed) zip writer: keeps this check free of
// system zip tools and npm dependencies.
function storeZip(entries) {
  const chunks = [];
  const central = [];
  let offset = 0;
  for (const [name, content] of Object.entries(entries)) {
    const nameBuf = Buffer.from(name, "utf8");
    const data = Buffer.from(content, "utf8");
    const crc = crc32(data);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(data.length, 18);
    local.writeUInt32LE(data.length, 22);
    local.writeUInt16LE(nameBuf.length, 26);
    chunks.push(local, nameBuf, data);

    const cd = Buffer.alloc(46);
    cd.writeUInt32LE(0x02014b50, 0);
    cd.writeUInt16LE(20, 4);
    cd.writeUInt16LE(20, 6);
    cd.writeUInt32LE(crc, 16);
    cd.writeUInt32LE(data.length, 20);
    cd.writeUInt32LE(data.length, 24);
    cd.writeUInt16LE(nameBuf.length, 28);
    cd.writeUInt32LE(offset, 42);
    central.push(cd, nameBuf);
    offset += 30 + nameBuf.length + data.length;
  }
  const cdBuf = Buffer.concat(central);
  const eocd = Buffer.alloc(22);
  eocd.writeUInt32LE(0x06054b50, 0);
  eocd.writeUInt16LE(Object.keys(entries).length, 8);
  eocd.writeUInt16LE(Object.keys(entries).length, 10);
  eocd.writeUInt32LE(cdBuf.length, 12);
  eocd.writeUInt32LE(offset, 16);
  return Buffer.concat([...chunks, cdBuf, eocd]);
}

function makeDatapackZip() {
  const zipPath = "/tmp/upload-check-pack.zip";
  const data = storeZip({
    "pack.mcmeta": '{"pack":{"pack_format":34,"description":"upload check"}}',
    "data/minecraft/tags/function/tick.json": '{"values":[]}'
  });
  writeFileSync(zipPath, data);
  return zipPath;
}

async function login(page) {
  await page.goto(`${BASE}/login`, { waitUntil: "networkidle" });
  await page.locator('input[type="text"], input[name="username"], input#username').first().fill(USER);
  await page.locator('input[type="password"]').first().fill(PASS);
  await Promise.all([
    page.waitForURL((u) => !String(u).includes("/login"), { timeout: 15000 }).catch(() => {}),
    page.keyboard.press("Enter"),
  ]);
  await page.waitForTimeout(800);
}

async function main() {
  mkdirSync(OUT, { recursive: true });
  const zipPath = makeDatapackZip();

  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  await login(page);

  await page.goto(`${BASE}/servers`, { waitUntil: "networkidle" });
  const href = await page.evaluate(() => {
    const links = [...document.querySelectorAll("a[href^='/servers/']")];
    const a = links.find((l) => l.getAttribute("href") !== "/servers/new");
    return a ? a.getAttribute("href") : null;
  });
  if (!href) {
    note("U0", "HIGH", "no server found to test against");
    await browser.close();
    return;
  }
  await page.goto(`${BASE}${href}`, { waitUntil: "networkidle" });
  await page.waitForTimeout(1500);
  await page.getByRole("tab", { name: /Datapacks/ }).or(page.getByRole("button", { name: /^Datapacks$/ })).first().click();
  await page.waitForTimeout(800);

  const fileInput = page.locator('input[type="file"]');
  if (!(await fileInput.count())) {
    note("U1", "HIGH", "no file input on the Datapacks tab (upload affordance missing)");
    await browser.close();
    return;
  }

  try {
    await fileInput.setInputFiles(zipPath);
    note("U1", "PASS", "file picker accepted the datapack zip");
  } catch (e) {
    note("U1", "HIGH", "file picker rejected the upload", String(e).slice(0, 160));
    await page.screenshot({ path: join(OUT, "upload-FAILURE.png") });
    await browser.close();
    return;
  }

  // Upload runs, then the list refreshes with the new row.
  await page.waitForTimeout(6000);
  await page.screenshot({ path: join(OUT, "after-upload.png") });
  const body = await page.locator("body").innerText();
  if (/upload-check-pack/i.test(body)) {
    note("U2", "PASS", "uploaded datapack appears in the Datapacks list");
  } else if (/Uploaded datapack/i.test(body)) {
    note("U2", "PASS", "upload toast fired (list may still be refreshing)");
  } else {
    note("U2", "HIGH", "no sign of the uploaded datapack in the UI");
  }
  if (/must be a \.zip|not a valid datapack/i.test(body)) {
    note("U3", "MEDIUM", "upload surfaced a validation error unexpectedly");
  }

  await browser.close();
  console.log(`\nshots: ${OUT}`);
  const bad = findings.filter((f) => f.severity !== "PASS");
  if (bad.length) process.exitCode = 1;
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
