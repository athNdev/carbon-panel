# Carbon Panel contributor guide

Carbon Panel is a Go service and SvelteKit application for managing isolated
Minecraft servers through Docker. The project is developed and deployed in
multiple environments; changes must not depend on one developer workstation or
cluster.

## Build and verification

- Backend tests: `go test ./...` (`make test`).
- Backend static checks: `go vet ./...`.
- Frontend: `cd web/carbon-panel && bun install --frozen-lockfile && bun run check && bun run test`.
- Production frontend build: `bash scripts/web-build.sh`.
- Proto lint: `make proto-lint`.

Run the narrowest relevant checks while iterating, then run the checks for every
affected surface before merging. Add regression coverage for fixed bugs at the
closest practical integration seam.

## Project boundaries

- Keep developer workstation configuration, credentials, generated state, and
  deployment-specific addresses out of tracked source and examples.
- Document stable architectural choices in `docs/adr/` and user-facing
  operations in `docs/`.
- Treat Minecraft server worlds and databases as persistent user data. Build,
  development, and cleanup commands must preserve them unless a separately
  named destructive command is explicitly invoked.
- Do not broaden container privileges beyond the capabilities required by the
  server image. Test the effective Docker configuration, including operator
  overrides.
- Keep authentication and authorization fail-closed. Add coverage for new RPCs
  and resource scopes.

## Code and deployment changes

- Keep changes portable across supported operating systems and deployment
  modes; use configuration instead of machine-specific paths or addresses.
- Update Compose, Docker, Helm, Terraform, and release documentation when their
  behavior changes.
- Keep generated files reproducible from the checked-in source and pinned tool
  versions where feasible.
- Never commit secrets, local databases, server worlds, or build artifacts.
