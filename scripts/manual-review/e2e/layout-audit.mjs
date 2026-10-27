#!/usr/bin/env node
// SPDX-License-Identifier: MPL-2.0
//
// Layout clipping audit: for the main app pages, reports containers whose
// content overflows horizontally (clipped or scrollable) and elements that
// poke past the viewport. Catches table columns and tab strips cut off at
// the content edge — a class of bug screenshots show but DOM checks miss.
//
// Usage: node layout-audit.mjs

import { chromium } from "playwright";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const BASE = process.env.BASE_URL || "http://localhost:18081";
const USER = process.env.REVIEW_USER || "review";
const PASS = process.env.REVIEW_PASS || "CarbonReview123!";
const OUT = join(process.cwd(), "out", `layout-${new Date().toISOString().replace(/[:.]/g, "-")}`);

const PAGES = [
  ["dashboard", "/"],
  ["servers", "/servers"],
  ["server-detail", "/servers/__FIRST__"],
  ["studio", "/modpacks/studio/dp-demo"],
  ["wizard", "/servers/new"],
];

const findings = [];

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

async function audit(page, label) {
  const result = await page.evaluate(() => {
    const vw = document.documentElement.clientWidth;
    const clipped = [];
    for (const el of document.querySelectorAll("table, [role='tablist'], nav, div")) {
      const style = getComputedStyle(el);
      const overflowX = style.overflowX;
      const hidden = overflowX === "hidden" || overflowX === "clip";
      const scrollable = overflowX === "auto" || overflowX === "scroll";
      if (el.scrollWidth > el.clientWidth + 2 && el.clientWidth > 40) {
        clipped.push({
          tag: el.tagName.toLowerCase(),
          cls: (el.className || "").toString().slice(0, 60),
          client: el.clientWidth,
          scroll: el.scrollWidth,
          overflowX,
          verdict: hidden ? "CLIPPED" : scrollable ? "scrollable" : "overflowing",
          sample: (el.innerText || "").replace(/\s+/g, " ").slice(0, 60),
        });
      }
    }
    const pastViewport = [];
    for (const el of document.querySelectorAll("main *, [role='tablist'] *")) {
      const r = el.getBoundingClientRect();
      if (r.width > 0 && r.right > vw + 2 && el.children.length === 0) {
        pastViewport.push({
          tag: el.tagName.toLowerCase(),
          right: Math.round(r.right),
          vw,
          text: (el.textContent || "").trim().slice(0, 40),
        });
      }
    }
    return { vw, clipped, pastViewport };
  });
  findings.push({ label, ...result });
  console.log(`\n== ${label} (vw=${result.vw}) ==`);
  for (const c of result.clipped) {
    console.log(`  [${c.verdict}] <${c.tag}> ${c.cls} ${c.client}->${c.scroll} "${c.sample}"`);
  }
  for (const p of result.pastViewport.slice(0, 8)) {
    console.log(`  [PAST-VIEWPORT] <${p.tag}> right=${p.right}/${p.vw} "${p.text}"`);
  }
  await page.screenshot({ path: join(OUT, `${label}.png`) });
}

async function main() {
  mkdirSync(OUT, { recursive: true });
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  await login(page);

  // Resolve the first server id for the detail page.
  await page.goto(`${BASE}/servers`, { waitUntil: "networkidle" });
  	const firstHref = await page.evaluate(() => {
  		const links = [...document.querySelectorAll("a[href^='/servers/']")];
  		const a = links.find((l) => l.getAttribute("href") !== "/servers/new");
  		return a ? a.getAttribute("href") : null;
  	});

  for (const [label, path] of PAGES) {
    const resolved = path.replace("__FIRST__", (firstHref || "").split("/").pop() || "");
    if (path.includes("__FIRST__") && !firstHref) continue;
    await page.goto(`${BASE}${resolved}`, { waitUntil: "networkidle" });
    await page.waitForTimeout(1500);
    await audit(page, label);
  }

  await browser.close();
  writeFileSync(join(OUT, "report.json"), JSON.stringify(findings, null, 2));
  console.log(`\nreport: ${OUT}`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
