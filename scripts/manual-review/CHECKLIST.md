# Manual review checklist

Record outcomes on the linked Jira items. Screenshots are optional evidence;
paste only redacted material.

## A. Shell and auth

| # | Steps | Expected |
| --- | --- | --- |
| A1 | Open http://localhost:18081, log in as `review` / `CarbonReview123!` | Dashboard renders; no blank screen |
| A2 | Open http://localhost:18082 and log in | Independent instance; separate data |
| A3 | Reload a deep route while using browser devtools "Offline" (PR #229) | `CarbonErrorState` with retry; never a blank page |

## B. UI fixes in PR #229

| # | Steps | Expected |
| --- | --- | --- |
| B1 | Tab through sidebar, server tabs, dialogs | Visible focus; focus trapped in dialogs; Escape closes |
| B2 | Inspect dark-surface text and icon-only buttons | Readable contrast; icon buttons have accessible names |
| B3 | Settings -> additional ports; resize to 390px; add an invalid row | Fields stack, errors are indexed and announced, nothing clips |
| B4 | Resize 1280 / 1024 / 768 / 390 on dashboard and server detail | No horizontal page scroll; tables scroll in-container |

## C. Ownership and quarantine (PR #230)

| # | Steps | Expected |
| --- | --- | --- |
| C1 | `docker exec <A> cat /app/data/installation-id` and same for B | Two different identities |
| C2 | In **B**, create a server (Vanilla, 1 GB, do not start) | Success; container carries B's installation label |
| C3 | `docker restart <A>`; open A's logs | A ignores B's container: log line "belongs to another Carbon Panel installation"; B's container still exists |
| C4 | `docker run -d --name review-orphan --label carbon-panel.managed=true --label carbon-panel.server.id=demo --label carbon-panel.installation=<A id> alpine sleep 600`, then restart A | A logs "Quarantining server container"; container still running; nothing deleted |
| C5 | Confirm `reconcile_purge_orphans` is unset | No container removed by reconciliation |

## D. Durable intent and recovery (PR #233)

| # | Steps | Expected |
| --- | --- | --- |
| D1 | In A, start a server; immediately `docker restart <A>` during startup | On restart A logs an interrupted-operation summary; recovery observes the container; the server reaches a truthful state |
| D2 | After recovery, start then stop the same server | No "operation ... requires completion" errors; both succeed |
| D3 | `docker logs <A>` after a normal start | Journal completion is logged only on failure; no repeated recovery |

## E. Known gaps (reproduce, do not expect fixed)

| # | Steps | Current expected (Jira) |
| --- | --- | --- |
| E1 | Dashboard "Recent Activity"; start or wait for deep sleep | Red pip for non-error states (OSS-229 sibling item: MINE-294) |
| E2 | Open Changelog | Cloud tab present; markdown rendered as raw `#` text (MINE-296) |
| E3 | Inspect the header | "CARBON UI" badge, legacy logo, no profile menu (MINE-295) |
| E4 | Switch server detail tabs | Choppy indicator/content transition (MINE-297) |
| E5 | Modpack Studio at ~880px | Card action buttons clipped (MINE-298) |

## F. Docs (PR #226)

| # | Steps | Expected |
| --- | --- | --- |
| F1 | Open Changelog and compare with `CHANGELOG.md` | Content matches; links resolve |
| F2 | Skim `docs/plans/oss-live-acceptance-matrix.md` | Status claims match what you just observed |

## G. Windows firewall (outside the harness)

| # | Steps | Expected |
| --- | --- | --- |
| G1 | Remove any stale `services.test.exe` firewall rule, then run `go test ./internal/rpc/services` in an unelevated Windows shell | No firewall prompt |

## Teardown

```sh
MSYS_NO_PATHCONV=1 wsl.exe -e bash /mnt/c/research_learning_new/carbonpanel/scripts/manual-review/review.sh down
```
