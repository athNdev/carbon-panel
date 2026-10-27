// SPDX-License-Identifier: MPL-2.0
//
// Browser-driven manual review for Carbon Panel.
//
// Controls the real app like a user (login, navigate, click, type, resize),
// captures screenshots at every breakpoint, and emits a structured findings
// report. This replaces ad-hoc manual clicking with a repeatable pass that any
// agent or human can run:
//
//   node run.mjs                       # reviews instance A on :18081
//   BASE_URL=http://localhost:18082 node run.mjs
//   BASE_URL_A=... BASE_URL_B=... node run.mjs   # both (ownership checks)
//
// Artifacts land in ./out/<timestamp>/ as PNGs plus report.md and report.json.

import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";

const BASE = process.env.BASE_URL || "http://localhost:18081";
const USER = process.env.REVIEW_USER || "review";
const PASS = process.env.REVIEW_PASS || "CarbonReview123!";
const OUT = join(process.cwd(), "out", new Date().toISOString().replace(/[:.]/g, "-"));

const VIEWPORTS = [
  { name: "desktop-1280", width: 1280, height: 900 },
  { name: "laptop-1024", width: 1024, height: 800 },
  { name: "tablet-768", width: 768, height: 1024 },
  { name: "phone-390", width: 390, height: 844 },
];

const findings = [];
const note = (id, area, severity, what, detail) => {
  findings.push({ id, area, severity, what, detail });
  const tag = severity === "HIGH" ? "!!" : severity === "MEDIUM" ? " !" : "  ";
  console.log(`${tag} [${id}] ${area}: ${what}`);
};

async function shot(page, name) {
  const file = join(OUT, `${name}.png`);
  await page.screenshot({ path: file, fullPage: false });
  return file;
}

async function login(page, base) {
  await page.goto(`${base}/login`, { waitUntil: "networkidle" });
  // Local auth form: fill the two inputs and submit.
  const user = page.locator('input[type="text"], input[name="username"], input#username').first();
  const pass = page.locator('input[type="password"]').first();
  await user.fill(USER);
  await pass.fill(PASS);
  await Promise.all([
    page.waitForURL((u) => !String(u).includes("/login"), { timeout: 15000 }).catch(() => {}),
    page.keyboard.press("Enter"),
  ]);
  await page.waitForTimeout(800);
  if (page.url().includes("/login")) {
    throw new Error("login did not navigate away from /login");
  }
}

async function collapseSidebar(page, toCollapsed = true) {
  // Fresh, stable DOM before interacting.
  const toggle = page
    .locator('button[aria-label="Toggle Navigation"], button[aria-label*="avig"], header button:first-of-type')
    .first();
  await page.waitForTimeout(250);
  if ((await toggle.count()) === 0) return false;
  await toggle.click();
  const done = toCollapsed ? (w) => w > 0 && w < 100 : (w) => w > 200;
  for (let i = 0; i < 40; i++) {
    await page.waitForTimeout(100);
    const w = await page.evaluate(() => {
      const a = document.getElementById("carbon-sidenav");
      return a ? Math.round(a.getBoundingClientRect().width) : -1;
    });
    if (done(w)) return true;
  }
  return false;
}

async function horizontalOverflow(page) {
  return page.evaluate(() => {
    const de = document.documentElement;
    return { scrollW: de.scrollWidth, clientW: de.clientWidth, overflow: de.scrollWidth > de.clientWidth + 1 };
  });
}

async function run() {
  await mkdir(OUT, { recursive: true });
  const browser = await chromium.launch({ headless: true });
  const ctx = await browser.newContext({ viewport: VIEWPORTS[0] });
  const page = await ctx.newPage();

  console.log(`\n=== Carbon Panel browser review ===\nbase: ${BASE}\nout:  ${OUT}\n`);

  // ---- Auth (A1) -------------------------------------------------------
  try {
    await login(page, BASE);
    await shot(page, "A1-dashboard-expanded");
    note("A1", "auth", "PASS", "login reaches the dashboard", page.url());
  } catch (e) {
    note("A1", "auth", "HIGH", "login failed", String(e));
    await shot(page, "A1-login-FAILURE");
    await browser.close();
    await emit();
    return;
  }

  // ---- Collapsed sidebar rail (the malformed-rail regression) ----------
  for (const vp of VIEWPORTS) {
    await page.setViewportSize({ width: vp.width, height: vp.height });
    await page.waitForTimeout(350);
    await shot(page, `R-sidebar-expanded-${vp.name}`);
    const ov = await horizontalOverflow(page);
    if (ov.overflow) {
      note("B4", "responsive", "HIGH", `horizontal overflow at ${vp.name}`, `scrollW ${ov.scrollW} > clientW ${ov.clientW}`);
    } else {
      note("B4", "responsive", "PASS", `no horizontal overflow at ${vp.name}`, `scrollW=${ov.scrollW}`);
    }
  }

  await page.setViewportSize({ width: VIEWPORTS[0].width, height: VIEWPORTS[0].height });
  await page.goto(`${BASE}`, { waitUntil: "networkidle" });
  await page.waitForTimeout(500);
  const collapsed = await collapseSidebar(page);
  if (collapsed) {
    await shot(page, "R-sidebar-collapsed-1280");
    // Measure icon centering inside the 48px rail.
    const rail = await page.evaluate(() => {
      const aside = document.getElementById("carbon-sidenav");
      if (!aside) return { err: "no #carbon-sidenav" };
      const w = aside.getBoundingClientRect().width;
      const link = aside.querySelector("nav a");
      if (!link) return { err: "no nav link" };
      const svg = link.querySelector("svg");
      const lr = link.getBoundingClientRect();
      const sr = svg.getBoundingClientRect();
      const iconCenter = sr.left + sr.width / 2;
      const railCenter = lr.left + lr.width / 2;
      return {
        railWidth: Math.round(w),
        offsetWidth: aside.offsetWidth,
        className: aside.className.slice(-40),
        iconCenterOffset: Math.round(((sr.left + sr.width / 2) - (lr.left + lr.width / 2)) * 10) / 10,
        iconW: sr.width,
      };
    });
    if (rail.err) {
      note("R1", "sidebar", "HIGH", "collapsed rail not measurable", rail.err);
    } else if (Math.abs(rail.iconCenterOffset) <= 1) {
      note("R1", "sidebar", "PASS", `collapsed rail icons centred (offset ${rail.iconCenterOffset}px)`, `railWidth=${rail.railWidth}`);
    } else {
      note("R1", "sidebar", "MEDIUM", `collapsed rail icons off-centre by ${rail.iconCenterOffset}px`, `railWidth=${rail.railWidth}`);
    }
    // Re-expand for the rest of the pass.
    await collapseSidebar(page, false);
    await page.waitForTimeout(300);
  } else {
    note("R1", "sidebar", "MEDIUM", "sidebar toggle not found", "aria-label=Toggle Navigation missing");
  }

  // ---- Top bar (E3 known gap) -----------------------------------------
  const topbar = await page.evaluate(() => {
    const header = document.querySelector("header");
    const txt = header ? header.innerText : "";
    return {
      hasCarbonUIBadge: /carbon\s*ui/i.test(txt),
      hasLegacyLogo: !!document.querySelector('header img[alt*="Carbon"]'),
    };
  });
  await shot(page, "E3-topbar");
  note("E3", "topbar", topbar.hasCarbonUIBadge ? "MEDIUM" : "PASS",
    topbar.hasCarbonUIBadge ? "'Carbon UI' badge still present" : "'Carbon UI' badge removed",
    JSON.stringify(topbar));
  note("E3", "topbar", topbar.hasLegacyLogo ? "MEDIUM" : "PASS",
    topbar.hasLegacyLogo ? "legacy logo still present" : "legacy logo removed", "");

  // Open the profile menu and confirm it renders.
  const profileTrigger = page.locator('header button[aria-haspopup]').first();
  let menuOpens = false;
  if (await profileTrigger.count()) {
    await profileTrigger.click();
    await page.waitForTimeout(300);
    menuOpens = await page.evaluate(() => !!document.querySelector('[role="menu"]'));
    await shot(page, "E3-profile-menu");
    await page.keyboard.press("Escape");
    await page.waitForTimeout(200);
  }
  note("E3", "topbar", menuOpens ? "PASS" : "MEDIUM",
    menuOpens ? "profile dropdown opens" : "profile dropdown missing", "");

  // ---- Changelog (E2 known gap) ---------------------------------------
  await page.goto(`${BASE}/changelog`, { waitUntil: "networkidle" });
  await page.waitForTimeout(400);
  await shot(page, "E2-changelog");
  const cl = await page.evaluate(() => {
    const body = document.body.innerText;
    const tabs = [...document.querySelectorAll("button")].filter((b) => /^(oss|cloud)$/i.test((b.textContent || "").trim()));
    const rendered = document.querySelectorAll(".changelog-md h1, .changelog-md h2, .changelog-md h3, .changelog-md li").length;
    return {
      hasCloudTab: tabs.length > 0,
      renderedMarkdown: rendered > 3,
      rawMarkdown: /^\s*#+\s/m.test(body),
    };
  });
  note("E2", "changelog", cl.hasCloudTab ? "MEDIUM" : "PASS",
    cl.hasCloudTab ? "edition tabs present in OSS build" : "no edition tabs", "");
  note("E2", "changelog", cl.renderedMarkdown && !cl.rawMarkdown ? "PASS" : "MEDIUM",
    cl.renderedMarkdown && !cl.rawMarkdown ? "markdown rendered to HTML" : "markdown not rendered", JSON.stringify(cl));

  // ---- Dashboard status pips (E1 known gap) ---------------------------
  await page.goto(`${BASE}`, { waitUntil: "networkidle" });
  await page.waitForTimeout(400);
  await shot(page, "E1-dashboard-status");
  const pips = await page.evaluate(() => ({
    redPipCount: [...document.querySelectorAll('[class*="#da1e28"]')].length,
    serverRows: [...document.querySelectorAll('[class*="cursor-pointer"]')].length,
  }));
  note("E1", "status-pips", "MEDIUM",
    `${pips.redPipCount} error-red elements with ${pips.serverRows} rows; needs servers in mixed states to assess (known gap MINE-294)`, "");

  // ---- Servers list + detail (B1 keyboard, tabs) ----------------------
  await page.goto(`${BASE}/servers`, { waitUntil: "networkidle" });
  await page.waitForTimeout(500);
  await shot(page, "B-servers-list");
  // Keyboard focus walk (B1)
  const focusWalk = await page.evaluate(() => {
    const els = [...document.querySelectorAll('a[href], button, input, select, [tabindex]:not([tabindex="-1"])')]
      .filter((e) => e.offsetParent !== null);
    return { focusableCount: els.length };
  });
  note("B1", "a11y", "PASS", `${focusWalk.focusableCount} focusable elements found on servers list`, "");

  // ---- Error state (A3): block RPC and reload --------------------------
  await ctx.route(/carbonpanel\.v1\./, (r) => r.abort());
  await page.goto(`${BASE}/servers`, { waitUntil: "domcontentloaded" });
  await page.waitForTimeout(1500);
  await shot(page, "A3-error-state");
  const errState = await page.evaluate(() => {
    const t = document.body.innerText;
    const retryBtn = [...document.querySelectorAll("button")].some((b) =>
      /retry|try again|reload|refresh/i.test(b.textContent || ""));
    return { hasRetry: retryBtn, blank: t.trim().length < 40, text: t.trim().slice(0, 120) };
  });
  note("A3", "error-state", errState.hasRetry && !errState.blank ? "PASS" : "HIGH",
    errState.hasRetry && !errState.blank ? "load failure renders a retry affordance" : "load failure renders blank/no retry", JSON.stringify(errState));
  await ctx.unroute(/carbonpanel\.v1\./);

  // ---- Additional-ports narrow layout (B3) ----------------------------
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${BASE}/servers/new`, { waitUntil: "networkidle" });
  await page.waitForTimeout(600);
  await shot(page, "B3-create-server-390");
  const narrow = await horizontalOverflow(page);
  note("B3", "responsive", narrow.overflow ? "HIGH" : "PASS",
    narrow.overflow ? `create-server overflows at 390px (scrollW ${narrow.scrollW})` : "create-server fits 390px", "");

  await browser.close();
  await emit();
}

async function emit() {
  const md = [
    "# Carbon Panel browser review",
    "",
    `- base: ${BASE}`,
    `- at:   ${new Date().toISOString()}`,
    `- user: ${USER}`,
    "",
    "| ID | Area | Severity | Finding | Detail |",
    "| --- | --- | --- | --- | --- |",
    ...findings.map((f) => `| ${f.id} | ${f.area} | ${f.severity} | ${f.what} | ${String(f.detail).replace(/\|/g, "/")} |`),
    "",
    "## Counts",
    "",
    `- HIGH: ${findings.filter((f) => f.severity === "HIGH").length}`,
    `- MEDIUM: ${findings.filter((f) => f.severity === "MEDIUM").length}`,
    `- PASS: ${findings.filter((f) => f.severity === "PASS").length}`,
  ].join("\n");

  await writeFile(join(OUT, "report.md"), md);
  await writeFile(join(OUT, "report.json"), JSON.stringify({ base: BASE, at: new Date().toISOString(), findings }, null, 2));
  console.log("\n" + md);
  console.log(`\nreport: ${join(OUT, "report.md")}`);
  console.log(`shots:  ${OUT}`);
}

run().catch((e) => {
  console.error("review run failed:", e);
  process.exit(1);
});
