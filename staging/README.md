# Staging environment (discopanel-dev, CT108)

Docker-based staging next to the hand-rolled dev stack on `discopanel-dev`
(`192.168.0.118`, LXC 108 on prox3). No PaaS — plain `docker compose` plus
`staging.sh`, so it can't fight the dev hot-reload setup.

- **URL:** `http://192.168.0.118:8081` (dev keeps `:8080`/`:5173`/`:5174`)
- **Pinned track:** latest `origin/main`. Cron runs `staging.sh autopin`
  every 5 min and redeploys when main moves.
- **Branch testing (pre-merge):** `staging.sh switch <branch>` builds and
  deploys any branch. Autopin leaves a manual branch alone until you run
  `staging.sh pin` again.
- **Data:** `/opt/staging/{data,backups,tmp}` — fully separate from dev.
- **Deployed ref:** recorded in `/opt/staging/REF`.

## Setup (run on discopanel-dev, e.g. via `pct exec 108` from prox3)

```sh
mkdir -p /opt/staging
git clone https://github.com/athNdev/carbon-panel /opt/staging/src
cp /opt/staging/src/staging/docker-compose.yml /opt/staging/
cp /opt/staging/src/staging/staging.sh /usr/local/bin/staging.sh
chmod +x /usr/local/bin/staging.sh
mkdir -p /opt/staging/data /opt/staging/backups /opt/staging/tmp
staging.sh pin
echo '*/5 * * * * root /usr/local/bin/staging.sh autopin >> /var/log/staging-autopin.log 2>&1' \
  > /etc/cron.d/carbon-staging
```

## Files

- `docker-compose.yml` — equivalent service definition (reference; the host
  has no compose plugin, so `staging.sh` uses plain `docker run`)
- `staging.sh` — `pin | switch <branch> | autopin | status | logs`

## Notes

- After changing files in `staging/`, re-copy them on the box:
  `git -C /opt/staging/src show origin/main:staging/staging.sh > /usr/local/bin/staging.sh`
  (same for `docker-compose.yml` → `/opt/staging/`).
- The box needed a memory bump (4 → 6 GB) for image builds; dev stack
  untouched. `staging.sh switch` builds can take ~10 min on this host.
