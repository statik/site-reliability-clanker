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
base="http://127.0.0.1:${port}"
idp_port=7190
idp="http://127.0.0.1:${idp_port}"
jar="${work}/cookies"
core=""
provider=""
failures=0

cleanup() {
  if [ -n "${core}" ]; then kill "${core}" 2>/dev/null || true; fi
  if [ -n "${provider}" ]; then kill "${provider}" 2>/dev/null || true; fi
  docker rm -f "${database}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

fail() {
  echo "FAILED: $1"
  failures=$((failures + 1))
}

# status prints the HTTP status of a request. The other arguments go to curl.
status() {
  curl -s -o /dev/null -w '%{http_code}' "$@"
}

# sign_in signs the browser of this check in as $1. $2 is "false" for an email address that
# the provider did not confirm. It prints the status of the last answer.
sign_in() {
  rm -f "${jar}"
  curl -fs -X POST "${idp}/person?email=$1&verified=${2:-true}"
  status -L -c "${jar}" -b "${jar}" "${base}/auth/login"
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
go build -o "${work}/fakeidp" ./scripts/verify/fakeidp

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
unset OPENROUTER_API_KEY CLANKER_SIGNING_KEY CLANKER_OIDC_ISSUER CLANKER_OIDC_CLIENT_ID \
  CLANKER_OIDC_CLIENT_SECRET CLANKER_PUBLIC_URL CLANKER_ADMIN_EMAIL

echo "== the core refuses a start without its key, with bad keys, with --data, without a sign-in"
refused "needs the signing key"
CLANKER_SIGNING_KEY="$(printf short | base64)" refused "has 5 bytes"
CLANKER_SIGNING_KEY="x" refused "remove --data" --data ./data
CLANKER_SIGNING_KEY="$(head -c 64 /dev/urandom | base64 | tr -d '\n')" refused "is not an Ed25519 private key"
CLANKER_SIGNING_KEY="$("${repo}/scripts/verify/signing-key.sh")"
export CLANKER_SIGNING_KEY
refused "people must sign in"

echo "== start a fake identity provider and the core"
"${work}/fakeidp" --addr "127.0.0.1:${idp_port}" >"${work}/idp.log" 2>&1 &
provider=$!
for _ in $(seq 1 30); do
  if curl -fs -o /dev/null "${idp}/.well-known/openid-configuration"; then break; fi
  sleep 1
done
export CLANKER_OIDC_ISSUER="${idp}" CLANKER_OIDC_CLIENT_ID=clanker CLANKER_OIDC_CLIENT_SECRET=check \
  CLANKER_PUBLIC_URL="${base}" CLANKER_ADMIN_EMAIL=admin@example.com
"${work}/clanker" up --cloud aws --env prod --demo=false --addr "127.0.0.1:${port}" \
  >"${work}/up.log" 2>&1 &
core=$!
for _ in $(seq 1 30); do
  if curl -fs -o /dev/null "${base}/healthz"; then break; fi
  sleep 1
done

echo "== nobody gets in without a sign-in"
for path in /api/v1/work-items /api/v1/events/stream "/git/runbooks.git/info/refs?service=git-upload-pack"; do
  if [ "$(status -m 5 -H 'X-Clanker-User: carol' "${base}${path}")" != 401 ]; then
    fail "${path} did not answer 401 to a request with no sign-in and a user header"
  fi
done
if [ "$(status "${base}/")" != 302 ]; then fail "the dashboard did not send a browser to the sign-in"; fi
if [ "$(sign_in mallory@example.com false)" != 403 ]; then
  fail "a person with an email address that is not confirmed signed in"
fi
sign_in stranger@example.com >/dev/null
if [ "$(status -b "${jar}" "${base}/api/v1/work-items")" != 403 ]; then
  fail "a signed-in person with no user did not get 403"
fi

echo "== the first admin signs in"
if [ "$(sign_in admin@example.com)" != 200 ]; then fail "the first admin could not sign in"; fi
if [[ "$(curl -fs -b "${jar}" "${base}/api/v1/me")" != *'"role":"admin"'* ]]; then
  fail "the signed-in person is not the admin"
fi
if [[ "$(curl -fs -b "${jar}" "${base}/api/v1/users")" == *alice* ]]; then
  fail "the core on a cloud has the demo users"
fi
page="$(curl -fs -b "${jar}" "${base}/" || true)"
script="$(grep -o '/assets/[^"]*\.js' <<<"${page}" | head -n 1 || true)"
if [ -z "${script}" ] || ! curl -fs -b "${jar}" -o /dev/null "${base}${script}"; then
  fail "the dashboard and its script do not answer"
fi
if [ "$(curl -fs -b "${jar}" "${base}/api/v1/work-items")" != "[]" ]; then
  fail "the core on a cloud proposed a work item"
fi

echo "== a program signs in with the device flow, and git uses its token"
unset HOME
client="${work}/client"
mkdir "${client}"
HOME="${client}" "${work}/clanker" login --server "${base}" >"${work}/login.log" 2>&1 &
login=$!
for _ in $(seq 1 30); do
  if grep -q "Approve only" "${work}/login.log"; then break; fi
  sleep 1
done
code="$(grep -o '[A-Z]\{4\}-[A-Z]\{4\}' "${work}/login.log" | head -n 1 || true)"
if [ "$(status "${base}/auth/device?user_code=${code}")" != 302 ]; then
  fail "the page of the code did not send a person with no sign-in to the sign-in"
fi
if [ "$(status -b "${jar}" -d "user_code=${code}" -d decision=approve "${base}/auth/device")" != 200 ]; then
  fail "the admin could not approve the code ${code}"
fi
if ! wait "${login}"; then fail "clanker login did not end well; see ${work}/login.log"; fi
token="$(HOME="${client}" "${work}/clanker" token --server "${base}" || true)"
if [[ "$(curl -s -H "Authorization: Bearer ${token}" "${base}/api/v1/me")" != *'"role":"admin"'* ]]; then
  fail "the token of the program does not act as the admin"
fi
if [ "$(status -H "Authorization: Bearer ${token}" -d "user_code=${code}" -d decision=approve "${base}/auth/device")" != 401 ]; then
  fail "a program with a token decided on a code"
fi

echo "== compare the seeded repositories with the checkout"
# git has no credential but the helper, which gives the token of clanker login.
as_program=(env HOME="${client}" GIT_TERMINAL_PROMPT=0 git -c credential.helper=
  -c "credential.${base}.helper=!${work}/clanker git-credential")
if "${as_program[@]}" -c "credential.${base}.helper=" clone -q "${base}/git/runbooks.git" "${work}/refused" 2>>"${work}/clone.log"; then
  fail "git cloned a repository with no credential"
fi
"${as_program[@]}" clone -q "${base}/git/runbooks.git" "${work}/runbooks" 2>>"${work}/clone.log"
"${as_program[@]}" clone -q "${base}/git/platform-live.git" "${work}/platform-live" 2>>"${work}/clone.log"

echo "== the program pushes a branch, and cannot push to main"
git -C "${work}/runbooks" checkout -q -b core-check
echo "written by a program" >"${work}/runbooks/core-check.txt"
git -C "${work}/runbooks" add core-check.txt
git -C "${work}/runbooks" -c user.name=admin -c user.email=admin@example.com commit -q -m "Check a push"
if ! (cd "${work}/runbooks" && "${as_program[@]}" push -q origin core-check 2>>"${work}/clone.log"); then
  fail "the program could not push a branch with its token"
fi
if (cd "${work}/runbooks" && "${as_program[@]}" push -q origin core-check:main 2>>"${work}/clone.log"); then
  fail "the program pushed to main"
fi
if [ "$("${as_program[@]}" ls-remote "${base}/git/runbooks.git" refs/heads/main | cut -f 1)" != "$(git -C "${work}/runbooks" rev-parse origin/main)" ]; then
  fail "main of the runbooks repository moved"
fi
git -C "${work}/runbooks" checkout -q main

git -C "${work}/runbooks" fsck --strict --no-dangling >/dev/null || fail "fsck of runbooks"
git -C "${work}/platform-live" fsck --strict --no-dangling >/dev/null || fail "fsck of platform-live"
# The core seeds runbooks/ of its source files and, in jobs/, the runbooks of the one pack in
# apps/ of the bundle, from the directory that the manifest of the pack names (seedForge in
# cmd/clanker/up.go).
manifest="$(tar -tzf "${repo}/internal/bundle/bundle.tar.gz" | grep -E '^apps/[^/]+/pack\.json$')"
pack_runbooks="$(git -C "${repo}" show "HEAD:${manifest}" |
  python3 -c 'import json, sys; print(json.load(sys.stdin).get("runbooks", ""))')"
jobs_tree=""
if [ -n "${pack_runbooks}" ]; then
  jobs_tree="$(git -C "${repo}" rev-parse "HEAD:${manifest%/pack.json}/${pack_runbooks}")"
fi
expected_runbooks="$({
  git -C "${repo}" ls-tree HEAD:runbooks
  if [ -n "${jobs_tree}" ]; then printf '040000 tree %s\tjobs\n' "${jobs_tree}"; fi
} | git -C "${work}/runbooks" mktree --missing)"
if [ "$(git -C "${work}/runbooks" rev-parse 'HEAD^{tree}')" != "${expected_runbooks}" ]; then
  fail "the runbooks repository is not runbooks/ of the checkout with the pack runbooks in jobs/"
fi
if [ "$(git -C "${work}/platform-live" rev-parse 'HEAD:platform')" != "$(git -C "${repo}" rev-parse 'HEAD:platform')" ]; then
  fail "platform/ in the platform-live repository is not platform/ in the checkout"
fi

if [ "$(status -X POST -b "${jar}" "${base}/auth/logout")" != 204 ] ||
  [ "$(status -b "${jar}" "${base}/api/v1/work-items")" != 401 ]; then
  fail "the session did not end at the logout"
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
echo "RESULT: the core signed in a person and a program, seeded the forge from memory and left no file"
