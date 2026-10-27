#!/bin/bash
# Runs on the server from the checkout: hard-sync to origin/main, rebuild, restart.
set -e
cd "$(dirname "$0")"
git symbolic-ref -q HEAD >/dev/null || { echo "checkout is on a detached HEAD: git switch main" >&2; exit 1; }
git fetch origin
git reset --hard origin/main
docker compose up -d --build --remove-orphans

port="${HOST_PORT:-8130}"
for _ in $(seq 1 30); do
  if curl -fsS "http://127.0.0.1:${port}/healthz" >/dev/null 2>&1; then
    echo "aptekamos-mcp deployed"
    exit 0
  fi
  sleep 1
done
echo "aptekamos-mcp did not answer on health check: docker compose logs --tail 50" >&2
exit 1
