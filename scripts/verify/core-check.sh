#!/usr/bin/env bash
# Start the core as it runs on a cloud, as a process on this machine, and check that it
# serves and leaves no file behind. It builds a binary that carries its source files, starts
# it outside a checkout against its own PostgreSQL container, and compares the repositories
# that the core seeds with the files of this checkout.
#
# Usage: scripts/verify/core-check.sh
#
# It looks for files in three places: the working directory, the home directory and the
# temporary directory of the core. It does not watch the rest of the disk. It does not
# touch the local demo.
set -euo pipefail

repo="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
database="clanker-core-check-$$"
port=7170
core=""
failures=0

cleanup() {
  if [ -n "${core}" ]; then kill "${core}" 2>/dev/null || true; fi
  docker rm -f "${database}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

fail() {
  echo "FAILED: $1"
  failures=$((failures + 1))
}

# refused runs the core with the given arguments, which must stop with the given message.
refused() {
  local want="$1"
  shift
  local out
  if out="$("${work}/clanker" up --cloud aws --env prod --demo=false --addr "127.0.0.1:${port}" "$@" 2>&1)"; then
    fail "the core started, but it must stop with: ${want}"
  elif [[ "${out}" != *"${want}"* ]]; then
    fail "want an error with \"${want}\", got: ${out}"
  fi
}

echo "== build a binary that carries its source files"
cd "${repo}"
if [ -n "$(git status --porcelain)" ]; then
  echo "the checkout has changes that are not committed; the bundle holds only committed files"
  exit 1
fi
make bundle >"${work}/bundle.log" 2>&1
CGO_ENABLED=0 go build -tags bundle -trimpath -o "${work}/clanker" ./cmd/clanker

echo "== start a database"
docker run -d --name "${database}" -p 127.0.0.1::5432 -e POSTGRES_PASSWORD=check \
  -e POSTGRES_DB=clanker postgres:18.7 >/dev/null
address="$(docker port "${database}" 5432/tcp | head -n 1)"
for _ in $(seq 1 60); do
  if docker exec "${database}" pg_isready -U postgres -d clanker >/dev/null 2>&1; then break; fi
  sleep 1
done

mkdir "${work}/cwd" "${work}/tmp"
cd "${work}/cwd"
export HOME="${work}/home-that-does-not-exist"
export TMPDIR="${work}/tmp"
export CLANKER_DATABASE_URL="postgres://postgres:check@${address}/clanker?sslmode=disable"
unset OPENROUTER_API_KEY CLANKER_SIGNING_KEY

echo "== the core refuses a start without its key, with a bad key, and with --data"
refused "needs the signing key"
CLANKER_SIGNING_KEY="$(printf short | base64)" refused "has 5 bytes"
CLANKER_SIGNING_KEY="x" refused "remove --data" --data ./data

echo "== start the core"
CLANKER_SIGNING_KEY="$(head -c 64 /dev/urandom | base64 | tr -d '\n')"
export CLANKER_SIGNING_KEY
"${work}/clanker" up --cloud aws --env prod --demo=false --addr "127.0.0.1:${port}" \
  >"${work}/up.log" 2>&1 &
core=$!
for _ in $(seq 1 30); do
  if curl -fs -o /dev/null "http://127.0.0.1:${port}/api/v1/work-items"; then break; fi
  sleep 1
done

page="$(curl -fs "http://127.0.0.1:${port}/" || true)"
script="$(grep -o '/assets/[^"]*\.js' <<<"${page}" | head -n 1 || true)"
if [ -z "${script}" ] || ! curl -fs -o /dev/null "http://127.0.0.1:${port}${script}"; then
  fail "the dashboard and its script do not answer"
fi
if [ "$(curl -fs "http://127.0.0.1:${port}/api/v1/work-items")" != "[]" ]; then
  fail "the core on a cloud proposed a work item"
fi

echo "== compare the seeded repositories with the checkout"
unset HOME
git clone -q "http://127.0.0.1:${port}/git/runbooks.git" "${work}/runbooks"
git clone -q "http://127.0.0.1:${port}/git/platform-live.git" "${work}/platform-live"
git -C "${work}/runbooks" fsck --strict --no-dangling >/dev/null || fail "fsck of runbooks"
git -C "${work}/platform-live" fsck --strict --no-dangling >/dev/null || fail "fsck of platform-live"
if [ "$(git -C "${work}/runbooks" rev-parse 'HEAD^{tree}')" != "$(git -C "${repo}" rev-parse 'HEAD:runbooks')" ]; then
  fail "the tree of the runbooks repository is not the tree of runbooks/ in the checkout"
fi
if [ "$(git -C "${work}/platform-live" rev-parse 'HEAD:platform')" != "$(git -C "${repo}" rev-parse 'HEAD:platform')" ]; then
  fail "platform/ in the platform-live repository is not platform/ in the checkout"
fi

kill "${core}"
wait "${core}" 2>/dev/null || true
core=""

echo "== files that the core left behind"
if [ -n "$(ls -A "${work}/cwd")" ]; then fail "files in the working directory: $(ls -A "${work}/cwd")"; fi
if [ -n "$(ls -A "${work}/tmp")" ]; then fail "files in the temporary directory: $(ls -A "${work}/tmp")"; fi
if [ -e "${work}/home-that-does-not-exist" ]; then fail "the core made the home directory"; fi
if grep -q "level=ERROR" "${work}/up.log"; then
  fail "error lines in the log of the core; see ${work}/up.log"
fi

if [ "${failures}" -ne 0 ]; then
  echo "RESULT: ${failures} checks failed. The files are in ${work}"
  exit 1
fi
echo "RESULT: the core served the dashboard, seeded the forge from memory and left no file"
