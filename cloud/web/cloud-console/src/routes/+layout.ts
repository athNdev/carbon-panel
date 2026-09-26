// Pure SPA: no SSR, no prerendered server content. adapter-static serves the
// fallback index.html and the app boots entirely client-side.
export const ssr = false;
export const csr = true;
export const prerender = true;