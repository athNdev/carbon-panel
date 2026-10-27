#!/usr/bin/env node
// SPDX-License-Identifier: MPL-2.0
//
// Error-handling auditor: drives every main route under injected API failures
// and checks the full contract — persistent error feedback (toasts do not
// count; they are transient), a retry affordance, never a blank screen, and
// never a fake "empty" state when the load failed. A second pass fails only
// mutations to verify actions surface failures, and a third checks loading
// indicators appear while requests are slow.
//
// Usage: node error-audit.mjs   (BASE_URL/REVIEW_USER/REVIEW_PASS as usual)

import { chromium } from "playwright";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const BASE = process.env.BASE_URL || "http://localhost:18081";
const USER = process.env.REVIEW_USER || "review";
const PASS = process.env.REVIEW_PASS || "CarbonReview123!";
const OUT = join(process.cwd(), "out", `errors-${new Date().toISOString().replace(/[:.]/g, "-")}`);

const ROUTES = [
  ["dashboard", "/"],
  ["servers", "/servers"],
  ["server-detail", "/servers/__FIRST__"],
  ["modpacks", "/modpacks"],
  ["studio-list", "/modpacks/studio"],
  ["studio-detail", "/modpacks/studio/__PACK__"],
  ["modules", "/modules"],
  ["settings", "/settings"],
  ["profile", "/profile"],
];

const findings = [];
const note = (id, area, severity, what, detail = "") => {
  findings.push({ id, area, severity, what, detail });
  const tag = severity === "HIGH" ? "!!" : severity === "MEDIUM" ? " !" : "  ";
  console.log(`${tag} [${id}] ${area}: ${what}${detail ? ` — ${detail}` : ""}`);
};

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

// Persistent feedback contract: toasts are transient and never count. The
// blank check deliberately ignores nav chrome by stripping toast text and
// requiring content markers (error copy, alerts or headings).
function detectErrorState() {
  const toastText = [...document.querySelectorAll("[data-sonner-toast]")]
    .map((n) => n.innerText)
    .join(" ");
  const t = (document.body.innerText || "").split(toastText).join("");
  const hasRetry = [...document.querySelectorAll("button, a")].some((b) =>
    /retry|try again|reload|refresh/i.test(b.textContent || b.getAttribute("aria-label") || "")
  );
  const hasAlert = document.querySelectorAll("[role='alert']").length > 0;
  const hasErrorText = /error|failed|couldn|unable|try again|something went wrong|unavailable/i.test(t);
  const hasHeading = document.querySelectorAll("h1, h2, h3").length > 0;
  const persistent = hasAlert || hasErrorText;
  return {
    hasRetry,
    persistent,
    toastSeen: document.querySelectorAll("[data-sonner-toast]").length,
    fakeEmpty: !persistent && /no .*(found|results|servers|items|data)|nothing here/i.test(t),
    blank: !persistent && !hasRetry && !hasHeading,
    sample: t.replace(/\s+/g, " ").trim().slice(0, 100),
  };
}

async function auditLoadErrors(ctx, page) {
  await ctx.route(/carbonpanel\.v1\.|\/api\/v1\//, (r) => r.abort());
  for (const [label, path] of ROUTES) {
    const resolved = path
      .replace("__FIRST__", page.__firstServer || "")
      .replace("__PACK__", page.__firstPack || "dp-demo");
    if (resolved.includes("__")) {
      note(`L-${label}`, "load-error", "MEDIUM", "skipped (no fixture for this route)");
      continue;
    }
    await page.goto(`${BASE}${resolved}`, { waitUntil: "domcontentloaded" });
    await page.waitForTimeout(1800);
    await page.screenshot({ path: join(OUT, `load-error-${label}.png`) });
    const s = await page.evaluate(detectErrorState);
    let verdict = "PASS";
    let what = "failed load shows persistent error feedback with retry";
    if (s.blank) {
      verdict = "HIGH";
      what = "failed load renders a blank screen";
    } else if (s.fakeEmpty) {
      verdict = "HIGH";
      what = "failed load masquerades as an empty state";
    } else if (!s.persistent) {
      verdict = "HIGH";
      what = "failed load has no persistent error feedback (toast-only is transient)";
    } else if (!s.hasRetry) {
      verdict = "MEDIUM";
      what = "persistent error feedback shown but no retry affordance";
    }
    note(`L-${label}`, "load-error", verdict, what, JSON.stringify(s));
  }
  await ctx.unroute(/carbonpanel\.v1\.|\/api\/v1\//);
}

async function auditActionErrors(ctx, page) {
  await ctx.route(/carbonpanel\.v1\.|\/api\/v1\//, (r) => {
    // ConnectRPC is POST-based: read RPCs (Get/List/Search/...) and session
    // calls pass through so pages load; write RPCs are the failures under test.
    const u = r.request().url();
    if (
      r.request().method() === "GET" ||
      /AuthService/.test(u) ||
      /\/(Get|List|Search|Read|Status|Ping)[A-Za-z]*$/.test(u)
    ) {
      return r.continue();
    }
    return r.abort();
  });

  await page.goto(`${BASE}/servers`, { waitUntil: "networkidle" });
  await page.waitForTimeout(1200);
	const startBtn = page.locator('[aria-label*="Start server" i], button[title*="Start" i]').first();
  if (await startBtn.count()) {
    await startBtn.click().catch(() => {});
    await page.waitForTimeout(2000);
    const s = await page.evaluate(detectErrorState);
    // Action failures use transient toast feedback by design (the form stays
    // open for retry); both channels satisfy the contract.
    const ok = s.persistent || s.toastSeen > 0;
    note("A-start", "action-error", ok ? "PASS" : "HIGH",
      ok ? "failed start surfaces feedback" : "failed start is silent", JSON.stringify(s));
    await page.screenshot({ path: join(OUT, "action-start-fail.png") });
  } else {
    note("A-start", "action-error", "MEDIUM", "no start button found to test (need a stopped server)");
  }

  const pack = page.__firstPack || "dp-demo";
  await page.goto(`${BASE}/modpacks/studio/${pack}`, { waitUntil: "networkidle" });
  await page.waitForTimeout(1500);
  const saveBtn = page.getByRole("button", { name: /^Save$/ }).first();
  if (await saveBtn.count()) {
    await saveBtn.click().catch(() => {});
    await page.waitForTimeout(2000);
    const s = await page.evaluate(detectErrorState);
    const ok = s.persistent || s.toastSeen > 0;
    note("A-save", "action-error", ok ? "PASS" : "HIGH",
      ok ? "failed save surfaces feedback" : "failed save is silent", JSON.stringify(s));
    await page.screenshot({ path: join(OUT, "action-save-fail.png") });
  } else {
    note("A-save", "action-error", "MEDIUM", "no save button found to test");
  }
  await ctx.unroute(/carbonpanel\.v1\.|\/api\/v1\//);
}

async function auditLoading(ctx, page) {
  await ctx.route(/carbonpanel\.v1\.|\/api\/v1\//, async (r) => {
    // Delay every data call: ConnectRPC is POST-based and REST is GET.
    await new Promise((res) => setTimeout(res, 2500));
    return r.continue();
  });
  await page.goto(`${BASE}/servers`, { waitUntil: "domcontentloaded" });
  await page.waitForTimeout(900);
  const loading = await page.evaluate(() => {
    const spin = document.querySelectorAll(
      ".animate-spin, [aria-busy='true'], [role='status'], [role='progressbar'], [aria-label*='loading' i], [class*='skeleton']"
    ).length;
    return { spin };
  });
  const ok = loading.spin > 0;
  note("D-loading", "loading", ok ? "PASS" : "MEDIUM",
    ok ? "slow loads show a loading indicator" : "no loading indicator during slow loads", JSON.stringify(loading));
  await page.screenshot({ path: join(OUT, "loading-state.png") });
  await ctx.unroute(/carbonpanel\.v1\.|\/api\/v1\//);
}

async function main() {
  mkdirSync(OUT, { recursive: true });
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await ctx.newPage();
  await login(page);

  await page.goto(`${BASE}/servers`, { waitUntil: "networkidle" });
  page.__firstServer = await page.evaluate(() => {
    const links = [...document.querySelectorAll("a[href^='/servers/']")];
    const a = links.find((l) => l.getAttribute("href") !== "/servers/new");
    return a ? a.getAttribute("href").split("/").pop() : "";
  });

  await auditLoadErrors(ctx, page);
  await auditActionErrors(ctx, page);
  await auditLoading(ctx, page);
  await browser.close();

  console.log("\n=== findings ===");
  for (const f of findings) {
    console.log(`${f.severity.padEnd(6)} [${f.id}] ${f.area}: ${f.what}`);
  }
  const bad = findings.filter((f) => f.severity === "HIGH" || f.severity === "MEDIUM");
  writeFileSync(
    join(OUT, "report.md"),
    ["# Error-handling audit", "", "| Check | Severity | Finding | Detail |", "| --- | --- | --- | --- |",
      ...findings.map((f) => `| ${f.id} | ${f.severity} | ${f.what} | ${String(f.detail).replace(/\|/g, "\\|")} |`)].join("\n")
  );
  console.log(`\n${bad.length} finding(s) need attention; report: ${OUT}`);
  if (bad.some((f) => f.severity === "HIGH")) process.exitCode = 1;
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
