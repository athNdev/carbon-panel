#!/usr/bin/env node
// SPDX-License-Identifier: MPL-2.0
//
// Status-pip audit: screenshots the servers list while servers are in mixed
// states and reports each row's pip color vs its status, so semantic pip
// colors can be verified against real lifecycle states (MINE-294).
//
// Usage: node pip-check.mjs   (BASE_URL/REVIEW_USER/REVIEW_PASS as usual)

import { chromium } from "playwright";
import { mkdirSync } from "node:fs";
import { join } from "node:path";

const BASE = process.env.BASE_URL || "http://localhost:18081";
const USER = process.env.REVIEW_USER || "review";
const PASS = process.env.REVIEW_PASS || "CarbonReview123!";
const OUT = join(process.cwd(), "out", `pips-${new Date().toISOString().replace(/[:.]/g, "-")}`);

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

async function readRows(page) {
  return page.evaluate(() => {
    const out = [];
    for (const el of document.querySelectorAll("a[href^='/servers/'], [data-server-row]")) {
      const text = el.innerText.replace(/\s+/g, " ").trim();
      const pip = el.querySelector("span[class*='rounded-full'], [class*='pip'], [aria-label*='status' i]");
      const style = pip ? getComputedStyle(pip).backgroundColor : null;
      out.push({ text: text.slice(0, 120), pipColor: style });
    }
    return out;
  });
}

async function main() {
  mkdirSync(OUT, { recursive: true });
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  await login(page);

  for (const label of ["starting", "running"]) {
    await page.goto(`${BASE}/servers`, { waitUntil: "networkidle" });
    await page.waitForTimeout(1500);
    await page.screenshot({ path: join(OUT, `servers-${label}.png`) });
    console.log(`\n== ${label} ==`);
    for (const row of await readRows(page)) {
      console.log(JSON.stringify(row));
    }
    if (label === "starting") {
      // Give the live server time to finish booting, then capture the mix.
      await page.waitForTimeout(90000);
    }
  }
  await browser.close();
  console.log(`\nshots: ${OUT}`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
