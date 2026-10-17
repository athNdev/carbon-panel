# Lane `console` — Carbon Cloud console (SvelteKit + Clerk + Carbon)

Branch: `cloud/console`
Write scope: `cloud/web/cloud-console/**`
Read-only reference: `web/carbon-panel/**` (the OSS app — copy design tokens from it,
never import from it), `cloud/proto/cloud/v1/**` (contracts).

This lane has **no Go acceptance criteria**. Its acceptance is:

```sh
cd cloud/web/cloud-console
bun install
bun run check      # svelte-check, zero errors
bun run build      # adapter-static SPA build succeeds
bun test           # unit tests for pure logic
```

Generated TypeScript already exists at
`cloud/web/cloud-console/src/lib/proto/cloud/v1/*_pb.ts`. **Never edit or regenerate
it.** Import the service descriptors from the `*_pb.ts` files and the clients from
`@connectrpc/connect-web`.

## Stack (pin these)

SvelteKit 2 + Svelte 5 (runes only — no legacy stores, no `$:`), Vite 7,
Tailwind CSS v4 via `@tailwindcss/vite`, `@sveltejs/adapter-static` with
`fallback: 'index.html'` (SPA), TypeScript 5, `svelte-check` 4.

Runtime deps:
- `@bufbuild/protobuf` ^2.10.0, `@connectrpc/connect` ^2.1.0,
  `@connectrpc/connect-web` ^2.1.0
- `@clerk/clerk-js` (latest 5.x)

No component library. Build the handful of primitives you need.

## Design

Match the OSS Carbon Gray 100 look exactly — read `web/carbon-panel/src/app.css`
and copy the `.dark` token block into your own `src/app.css` (dark is the only
theme). Non-negotiables:
- `--radius: 0px`; no rounded corners, no soft shadows, no pills.
- Background `#161616`, layer 01 `#262626`, layer 02 `#393939`, accent `#353535`,
  primary `#0f62fe`, danger `#da1e28`, success `#24a148`, warning `#f1c21b`,
  text `#f4f4f4`, muted text `#a8a8a8`, border `#393939`.
- IBM Plex Sans for UI, IBM Plex Mono for ids, ports, IPs, commands and logs
  (load from Google Fonts).
- 48px fixed top header, 256px collapsible left nav, 2px `#0f62fe` active
  indicator, 1px borders, 2x grid spacing.

Build these primitives in `src/lib/components/`:
`AppShell.svelte`, `Tile.svelte`, `Button.svelte` (primary/secondary/danger/ghost),
`Tag.svelte` (status colours), `DataTable.svelte` (sortable columns, empty state,
loading skeleton), `Tabs.svelte`, `Modal.svelte`, `Field.svelte` (label + input +
error, `aria-describedby` wired), `Toast.svelte`, `EmptyState.svelte`,
`CopyField.svelte` (monospace value + copy button, used for join commands and keys).

Accessibility is part of the acceptance: every input has an associated label, every
interactive element is keyboard reachable, status is never conveyed by colour alone
(the `Tag` also renders text), and the nav has proper `aria-current`.

## Auth

`src/lib/auth/clerk.svelte.ts`: a thin adapter over `@clerk/clerk-js`.

```ts
export const auth = createAuth();   // { ready, signedIn, user, org, orgRole,
                                    //   token(): Promise<string|null>,
                                    //   setActiveOrg(id), signOut() }
```

- `Clerk.load({ publishableKey })` where the key comes from
  `$env/dynamic/public.PUBLIC_CLERK_PUBLISHABLE_KEY`.
- Expose the session token for API calls and the active organization.
- When `PUBLIC_CLERK_PUBLISHABLE_KEY` is missing, the app must render a clear
  "Clerk is not configured" screen instead of crashing — this is required so the
  app builds and runs before the key is supplied (ADR 0006).
- `src/routes/+layout.svelte` guards everything except `/sign-in` and
  `/not-configured`: unauthenticated → redirect to `/sign-in`; authenticated with
  no active org → an org picker.
- `src/lib/auth/permissions.ts`: a **pure** module with
  `can(role: string, permission: string): boolean` and the role→permission table
  mirroring the server (owner > admin > operator > viewer; billing only billing
  permissions). Unit-test it with `bun test` — this is the only test required.

## API client

`src/lib/api/client.ts`:
- `PUBLIC_API_URL` (default `http://localhost:8080`).
- Create `Transport` from `@connectrpc/connect-web`'s `createConnectTransport`
  with an interceptor that attaches `Authorization: Bearer <clerk token>` and
  `X-Carbon-Org: <active org id>` when present.
- Export one typed client per service, built from the generated service
  descriptors: `sessionClient`, `orgClient`, `nodeClient`, `nodeTypeClient`,
  `provisionClient`, `workloadClient`, `roleClient`, `apiKeyClient`, `auditClient`,
  `systemClient`.
- A single `call()` wrapper that maps `ConnectError` codes to user-facing messages
  (`PermissionDenied` → "You do not have permission for this action",
  `Unauthenticated` → re-auth, `Unavailable` → retry banner).

## Pages (`src/routes/`)

| route | contents |
|---|---|
| `/sign-in` | Clerk sign-in / sign-up, Carbon-styled wrapper |
| `/` | overview: node count by status, workload count, recent audit events, capability warnings from `SystemService.GetCapabilities` (a disabled capability renders a visible banner naming the missing env keys) |
| `/nodes` | table: name, origin (BYO/managed), type, region, status, cpu/ram allocation bars, last heartbeat; row → detail |
| `/nodes/[id]` | node detail: capacity vs allocation, labels, telemetry, workloads on the node, drain/resume/delete actions |
| `/nodes/join` | BYO wizard: pick node type + name → `CreateJoinToken` → show the `join_command` in a `CopyField` with an expiry countdown and a "waiting for node" live poll |
| `/nodes/provision` | managed wizard: provider (only `configured` providers selectable, unconfigured ones show which env keys are missing) → region → node type (shows vcpu/ram/disk/price) → review → `CreateProvision` → `PlanProvision` → show the plan diff → `ApplyProvision` with an explicit confirm |
| `/node-types` | catalog table incl. per-provider instance type + price |
| `/workloads` | table: name, node, status, hostname, resources; row → detail |
| `/workloads/[id]` | spec, status, live console (poll `StreamWorkloadLogs` or a simple tail), start/stop/restart, delete, events list |
| `/members` | members table, role change, remove, invite (only shown when `can(role,'member.manage')`) |
| `/roles` | role catalog and the caller's effective permissions |
| `/api-keys` | list, create (show the secret exactly once in a `CopyField` with a warning), revoke |
| `/audit` | filterable audit log table (action prefix, resource type, actor, time range) |
| `/settings` | org name/slug, build info from `GetBuildInfo` |

Every page: loading skeleton, empty state, and an error state that shows the
server's message rather than a blank screen. Permission-denied actions must be
hidden or disabled **and** the server's denial must still be surfaced if it
happens anyway.

## Acceptance

```sh
cd cloud/web/cloud-console
bun install
bun run check    # zero errors
bun run build
bun test
```

Report per conventions §6, replacing the Go commands with the bun commands and
stating clearly whether `bun install` succeeded (it needs network).
