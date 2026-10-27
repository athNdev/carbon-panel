// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Carbon Panel landing page — progressive enhancement only.
// Everything on the page works without JS; this adds nav state,
// staggered scroll-reveal and copy-to-clipboard affordances.

(function () {
  "use strict";

  const prefersReduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  // --- Sticky nav: solidify after scrolling past the hero top ---
  const nav = document.querySelector(".nav");
  if (nav) {
    const onScroll = () => nav.classList.toggle("scrolled", window.scrollY > 8);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
  }

  // --- Mobile nav toggle ---
  const toggle = document.querySelector(".nav-toggle");
  const links = document.querySelector(".nav-links");
  if (toggle && links) {
    toggle.addEventListener("click", () => {
      const open = links.classList.toggle("open");
      toggle.setAttribute("aria-expanded", String(open));
    });
    links.addEventListener("click", (e) => {
      if (e.target.closest("a")) {
        links.classList.remove("open");
        toggle.setAttribute("aria-expanded", "false");
      }
    });
  }

  // --- Scroll reveal with ~100ms sibling stagger ---
  // Implemented with synchronous rect checks (not IntersectionObserver) so it
  // runs reliably in every environment and content is never left invisible.
  const revealEls = Array.from(document.querySelectorAll(".reveal"));

  function revealInView() {
    const vh = window.innerHeight || 0;
    revealEls.forEach((el) => {
      if (el.classList.contains("in")) return;
      const rect = el.getBoundingClientRect();
      // Reveal once the element crosses into the lower 88% of the viewport.
      // Elements already scrolled past also match (their top is negative), so
      // jump-scrolling or deep-linking never leaves content hidden.
      if (rect.top < vh * 0.88) {
        const parent = el.parentElement;
        const siblings = parent
          ? Array.from(parent.children).filter((c) => c.classList.contains("reveal"))
          : [el];
        const idx = Math.max(0, siblings.indexOf(el));
        el.style.setProperty("--reveal-delay", `${Math.min(idx, 6) * 90}ms`);
        el.classList.add("in");
      }
    });
  }

  if (prefersReduced) {
    revealEls.forEach((el) => el.classList.add("in"));
  } else if (revealEls.length) {
    revealInView(); // reveal whatever is already on screen
    let ticking = false;
    const schedule = () => {
      if (ticking) return;
      ticking = true;
      window.setTimeout(() => {
        ticking = false;
        revealInView();
      }, 80);
    };
    window.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    window.addEventListener("load", revealInView);
  }

  // --- Scroll-driven architecture section ---
  // Maps the pinned track's scroll progress to a data-beat state machine
  // plus a continuous --p for scrubbed line drawing.
  const arch = document.querySelector(".arch");
  if (arch && !prefersReduced) {
    const stage = arch.querySelector(".arch-stage");
    const track = arch.querySelector(".arch-track");
    const bar = arch.querySelector(".arch-progress-bar");
    let archTicking = false;
    const updateArch = () => {
      const rect = track.getBoundingClientRect();
      const total = track.offsetHeight - window.innerHeight;
      const p = total > 0 ? Math.min(1, Math.max(0, -rect.top / total)) : 1;
      stage.style.setProperty("--p", p.toFixed(4));
      if (bar) bar.style.transform = `scaleX(${p.toFixed(4)})`;
      const beat = p < 0.18 ? 0 : p < 0.38 ? 1 : p < 0.58 ? 2 : p < 0.78 ? 3 : 4;
      if (stage.dataset.beat !== String(beat)) stage.dataset.beat = String(beat);
      archTicking = false;
    };
    const scheduleArch = () => {
      if (archTicking) return;
      archTicking = true;
      window.requestAnimationFrame(updateArch);
    };
    window.addEventListener("scroll", scheduleArch, { passive: true });
    window.addEventListener("resize", scheduleArch);
    window.addEventListener("load", updateArch);
    updateArch();
  }

  // --- Footer year ---
  const yearEl = document.getElementById("year");
  if (yearEl) yearEl.textContent = String(new Date().getFullYear());

  // --- Live GitHub star count (graceful fallback: hidden if unavailable) ---
  const starEl = document.querySelector("[data-star-count]");
  if (starEl && window.fetch) {
    fetch("https://api.github.com/repos/athNdev/carbon-panel")
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (data && typeof data.stargazers_count === "number" && data.stargazers_count > 0) {
          starEl.textContent = data.stargazers_count.toLocaleString("en-US");
          starEl.hidden = false;
        }
      })
      .catch(() => {
        /* offline or rate-limited; leave the button without a count */
      });
  }

  // --- Copy-to-clipboard for install / code blocks ---
  const COPY_ICON =
    '<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/></svg>';
  const CHECK_ICON =
    '<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>';

  document.querySelectorAll("[data-copy]").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const target = document.getElementById(btn.getAttribute("data-copy"));
      if (!target) return;
      const text = target.innerText.trim();
      try {
        await navigator.clipboard.writeText(text);
      } catch {
        const ta = document.createElement("textarea");
        ta.value = text;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        try {
          document.execCommand("copy");
        } catch (_) {
          /* clipboard unavailable; leave button state unchanged */
        }
        document.body.removeChild(ta);
      }
      btn.classList.add("copied");
      const original = btn.innerHTML;
      btn.innerHTML = CHECK_ICON + "<span>Copied</span>";
      window.setTimeout(() => {
        btn.classList.remove("copied");
        btn.innerHTML = original;
      }, 1800);
    });
  });
})();
