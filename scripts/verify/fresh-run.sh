#!/usr/bin/env bash
# Destroy the local environment, start from an empty data directory, and run the first
# deployment and the ten scenes of the scripted month. It takes about 10 minutes and uses
# the live model when OPENROUTER_API_KEY is in .env.
#
# Usage: scripts/verify/fresh-run.sh
#
# It stops if the destroy fails, so that the state of a live environment is never thrown
# away. The approvals go through the API as the demo users alice and bob.
set -uo pipefail

repo="$(cd "$(dirname "$0")/../.." && pwd)"
keep="$(mktemp -d)"
cd "${repo}" || exit 1

pkill -f "clanker up" || true
pkill -f "clanker setup" || true
sleep 2

echo "== destroy the local environment"
make build >/dev/null
if ! ./bin/clanker destroy --env local >"${keep}/destroy.log" 2>&1; then
  echo "the destroy failed; the data directory is left as it is. See ${keep}/destroy.log"
  exit 1
fi

echo "== empty the data directory (the provider cache stays)"
if [ -d .clanker/plugin-cache ]; then mv .clanker/plugin-cache "${keep}/plugin-cache"; fi
for name in $(docker ps -a --filter "label=clanker.data=${repo}/.clanker" --format '{{.Names}}'); do
  docker rm -f "${name}" >/dev/null
  docker volume rm "${name}" >/dev/null
done
if [ -d .clanker ]; then trash .clanker; fi
mkdir -p .clanker
if [ -d "${keep}/plugin-cache" ]; then mv "${keep}/plugin-cache" .clanker/plugin-cache; fi

echo "== start the control plane"
make web >/dev/null 2>&1
nohup ./bin/clanker up >.clanker/up.log 2>&1 &
for _ in $(seq 1 60); do
  if curl -fs -o /dev/null http://127.0.0.1:7070/api/v1/work-items; then break; fi
  sleep 2
done
curl -s -o /dev/null -w "dashboard %{http_code}\n" http://127.0.0.1:7070/

echo "== first deployment and the scripted month"
python3 "${repo}/scripts/verify/month.py" quiet-week feature-release cve bad-release \
  kubernetes-eol database-patch database-upkeep restore-drill drift incident

echo "== error lines in the log of the control plane"
grep -c "level=ERROR" .clanker/up.log || true
