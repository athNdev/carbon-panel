#!/usr/bin/env node
// SPDX-License-Identifier: MPL-2.0
//
// Scripted datapack UI review: drives the real app like a user through the
// three datapack surfaces (server wizard step, studio overrides browser,
// server Datapacks tab), capturing screenshots and structured findings.
//
// Usage: node datapack-review.mjs   (instance A by default; set BASE_URL)

import { chromium } from "playwright";
import { mkdirSync } from "node:fs";
import { join } from "node:path";

const BASE = process.env.BASE_URL || "http://localhost:18081";
const USER = process.env.REVIEW_USER || "review";
const PASS = process.env.REVIEW_PASS || "CarbonReview123!";
const OUT = join(process.cwd(), "out", `datapack-${new Date().toISOString().replace(/[:.]/g, "-")}`);

const findings = [];
const note = (id, area, severity, what, detail = "") => {
  findings.push({ id, area, severity, what, detail });
  const tag = severity === "HIGH" ? "!!" : severity === "MEDIUM" ? " !" : "  ";
  console.log(`${tag} [${id}] ${area}: ${what}${detail ? ` — ${detail}` : ""}`);
};

async function shot(page, name) {
  const file = join(OUT, `${name}.png`);
  await page.screenshot({ path: file, fullPage: false });
  console.log(`   shot: ${file}`);
}

async function login(page) {
  await page.goto(`${BASE}/login`, { waitUntil: "networkidle" });
  const user = page.locator('input[type="text"], input[name="username"], input#username').first();
  const pass = page.locator('input[type="password"]').first();
  await user.fill(USER);
  await pass.fill(PASS);
  await Promise.all([
    page.waitForURL((u) => !String(u).includes("/login"), { timeout: 15000 }).catch(() => {}),
    page.keyboard.press("Enter"),
  ]);
  await page.waitForTimeout(800);
  if (page.url().includes("/login")) throw new Error("login failed");
}

async function reviewWizard(browser) {
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  page.on("console", (m) => {
    if (m.type() === "error") note("W-console", "wizard", "LOW", "console error", m.text().slice(0, 200));
  });
  try {
    await login(page);
    await page.goto(`${BASE}/servers/new`, { waitUntil: "networkidle" });
    await page.locator('input[placeholder*="name" i], input#name').first().fill("dp-wizard-test");
    for (let step = 0; step < 4; step++) {
      await page.getByRole("button", { name: /Next Step/ }).click();
      await page.waitForTimeout(400);
    }
    await shot(page, "wizard-step5-datapacks");

    const search = page.getByPlaceholder("Search datapacks...");
    await search.fill("farmers");
    await page.getByRole("button", { name: "Search", exact: true }).click();
    await page.waitForTimeout(2500);
    await shot(page, "wizard-step5-results");

    const addBtn = page.getByRole("button", { name: /^Add$/ }).first();
    if (await addBtn.count()) {
      await addBtn.click();
      await page.waitForTimeout(300);
      const selected = page.locator("text=Selected for install");
      note("W1", "wizard", (await selected.count()) ? "PASS" : "HIGH",
        (await selected.count()) ? "adding a datapack shows the selected list" : "selected list missing after Add");
      await shot(page, "wizard-step5-selected");
    } else {
      note("W1", "wizard", "HIGH", "no search results for 'farmers' (Add button absent)");
      await shot(page, "wizard-step5-no-results");
    }

    // Complete creation with one datapack selected; installs run post-create.
    await page.getByRole("button", { name: /Create Server/ }).click();
    try {
      await page.waitForURL((u) => /\/servers\/.+/.test(u.pathname) && !u.pathname.endsWith("/new"), { timeout: 60000 });
      note("W2", "wizard", "PASS", "wizard creates the server", page.url());
      await page.waitForTimeout(4000);
      const dpTab = page.getByRole("tab", { name: /Datapacks/ }).or(page.getByRole("button", { name: /^Datapacks$/ }));
      if (await dpTab.count()) {
        await dpTab.first().click();
        await page.waitForTimeout(1500);
        await shot(page, "wizard-created-server-datapacks");
        const body = await page.locator("body").innerText();
        if (/farmers/i.test(body)) {
          note("W3", "wizard", "PASS", "selected datapack installed into the new server");
        } else {
          note("W3", "wizard", "MEDIUM", "datapack not visible on server Datapacks tab after wizard install");
        }
      } else {
        note("W3", "wizard", "MEDIUM", "no Datapacks tab found on created server");
      }
    } catch (e) {
      note("W2", "wizard", "HIGH", "wizard did not navigate to the created server", String(e).slice(0, 200));
      await shot(page, "wizard-create-FAILURE");
    }
  } catch (e) {
    note("W0", "wizard", "HIGH", "wizard review crashed", String(e).slice(0, 200));
    await shot(page, "wizard-crash");
  } finally {
    await page.close();
  }
}

async function apiLogin() {
  const res = await fetch(`${BASE}/carbonpanel.v1.AuthService/Login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username: USER, password: PASS })
  });
  return (await res.json()).token;
}

// Ensure the studio fixture pack exists so the studio flow can run against
// any instance without manual setup.
async function ensureStudioPack() {
  const token = await apiLogin();
  await fetch(`${BASE}/api/v1/packwiz/packs`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify({ id: "dp-demo", name: "DP Demo", mc_version: "1.21.1", mod_loader: "fabric" })
  }).catch(() => {});
}

async function reviewStudio(browser) {
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  try {
    await login(page);
    await page.goto(`${BASE}/modpacks/studio/dp-demo`, { waitUntil: "networkidle" });
    await page.waitForTimeout(1500);
    const overridesTab = page.getByRole("button", { name: /Overrides & Configs/ }).or(page.getByRole("tab", { name: /Overrides & Configs/ }));
    if (await overridesTab.count()) {
      await overridesTab.first().click();
    } else {
      await page.locator("text=Overrides & Configs").first().click();
    }
    await page.waitForTimeout(1000);
    await shot(page, "studio-overrides");

    const browse = page.getByRole("button", { name: /Browse Datapacks/ }).first();
    if (!(await browse.count())) {
      note("S1", "studio", "HIGH", "Browse Datapacks button missing in Overrides tab");
      return;
    }
    await browse.click();
    await page.waitForTimeout(500);
    await page.getByPlaceholder("Search datapacks...").fill("biomes");
    await page.getByRole("button", { name: "Search", exact: true }).click();
    await page.waitForTimeout(2500);
    await shot(page, "studio-browse-dialog");

    const install = page.getByRole("button", { name: /Install/ }).first();
    if (await install.count()) {
      await install.click();
      await page.waitForTimeout(3000);
      await shot(page, "studio-after-install");
      const body = await page.locator("body").innerText();
      if (/datapacks\//.test(body)) {
        note("S2", "studio", "PASS", "installed datapack appears in the overrides list");
      } else {
        note("S2", "studio", "MEDIUM", "installed datapack not visible in overrides list");
      }
    } else {
      note("S2", "studio", "HIGH", "no search results in studio datapack dialog");
    }
  } catch (e) {
    note("S0", "studio", "MEDIUM", "studio review did not complete", String(e).slice(0, 200));
    await shot(page, "studio-crash");
  } finally {
    await page.close();
  }
}

async function reviewServerTab(browser) {
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  try {
    await login(page);
    await page.goto(`${BASE}/servers`, { waitUntil: "networkidle" });
    await page.locator("text=dp-live").first().click();
    await page.waitForTimeout(2000);
    const dpTab = page.getByRole("tab", { name: /Datapacks/ }).or(page.getByRole("button", { name: /^Datapacks$/ }));
    if (!(await dpTab.count())) {
      note("T1", "server-tab", "HIGH", "Datapacks tab missing on server detail");
      return;
    }
    await dpTab.first().click();
    await page.waitForTimeout(1000);
    await shot(page, "server-datapacks-tab");

    const browse = page.getByRole("button", { name: /Browse/ }).first();
    if (await browse.count()) {
      await browse.click();
      await page.waitForTimeout(500);
      await page.getByPlaceholder("Search datapacks...").fill("farmers");
      await page.getByRole("button", { name: "Search", exact: true }).click();
      await page.waitForTimeout(2500);
      await shot(page, "server-datapacks-dialog");
      const install = page.getByRole("button", { name: /Install/ }).first();
      if (await install.count()) {
        await install.click();
        await page.waitForTimeout(3000);
        await shot(page, "server-datapacks-installed");
        note("T2", "server-tab", "PASS", "install via server Datapacks dialog completed");
      } else {
        note("T2", "server-tab", "MEDIUM", "no results in server datapack dialog");
      }
    } else {
      note("T2", "server-tab", "HIGH", "no Browse button on Datapacks tab");
    }
  } catch (e) {
    note("T0", "server-tab", "HIGH", "server tab review crashed", String(e).slice(0, 200));
    await shot(page, "server-tab-crash");
  } finally {
    await page.close();
  }
}

async function main() {
  mkdirSync(OUT, { recursive: true });
  const browser = await chromium.launch();
  await ensureStudioPack();
  await reviewWizard(browser);
  await reviewStudio(browser);
  await reviewServerTab(browser);
  await browser.close();

  console.log("\n=== findings ===");
  for (const f of findings) {
    console.log(`${f.severity.padEnd(6)} [${f.id}] ${f.area}: ${f.what}`);
  }
  const bad = findings.filter((f) => f.severity === "HIGH" || f.severity === "MEDIUM");
  console.log(`\n${bad.length} finding(s) need attention; screenshots in ${OUT}`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
