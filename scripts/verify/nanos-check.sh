#!/usr/bin/env bash
# Boot the core as a Nanos unikernel under QEMU on this laptop, against its own PostgreSQL
# container. It checks the dashboard and the seeded forge, pushes a branch with the git
# client, then boots a second unikernel on the same database and looks for the branch.
#
# Usage: scripts/verify/nanos-check.sh
#
# Environment:
#   NANOS_ACCEL  hvf (default) or tcg; see ops/run-local.sh.
#
# It needs ops, QEMU and Docker. It is for an Apple Silicon Mac, as ops/run-local.sh is.
# The database address and the signing key go into the image as `Env`. That is correct
# only for this throwaway test: the image for a cloud account must not carry a secret.
set -euo pipefail

repo="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
database="clanker-nanos-check-$$"
port=7180
base="http://127.0.0.1:${port}"
failures=0

fail() {
  echo "FAILED: $1"
  failures=$((failures + 1))
}

stop_unikernel() {
  pkill -f "qemu-system-aarch64.*clanker-nanos-local" 2>/dev/null || true
  for _ in $(seq 1 20); do
    if ! pgrep -f "qemu-system-aarch64.*clanker-nanos-local" >/dev/null; then return; fi
    sleep 0.5
  done
}

cleanup() {
  stop_unikernel
  docker rm -f "${database}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# boot starts a unikernel and waits until the API answers. $1 is the name of its log.
boot() {
  local started
  started="$(date +%s)"
  NANOS_CONFIG="${work}/config.json" "${repo}/ops/run-local.sh" "${work}/clanker-nanos" "${port}" \
    >"${work}/$1.log" 2>&1 &
  for _ in $(seq 1 120); do
    if curl -fs -m 2 -o /dev/null "${base}/api/v1/work-items"; then
      echo "the API answered $(($(date +%s) - started)) s after the start of the image build"
      return
    fi
    sleep 1
  done
  fail "the API did not answer in 120 s; see ${work}/$1.log"
  tail -n 20 "${work}/$1.log"
  echo "RESULT: the unikernel did not start. The files are in ${work}"
  exit 1
}

echo "== build a static linux/arm64 binary that carries its source files"
cd "${repo}"
if [ -n "$(git status --porcelain)" ]; then
  echo "the checkout has changes that are not committed; the bundle holds only committed files"
  exit 1
fi
make bundle >"${work}/bundle.log" 2>&1
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags bundle -trimpath \
  -o "${work}/clanker-nanos" ./cmd/clanker

echo "== start a database"
docker run -d --name "${database}" -p 127.0.0.1::5432 -e POSTGRES_PASSWORD=check \
  -e POSTGRES_DB=clanker postgres:18.7 >/dev/null
address="$(docker port "${database}" 5432/tcp | head -n 1)"
for _ in $(seq 1 60); do
  if docker exec "${database}" pg_isready -U postgres -d clanker >/dev/null 2>&1; then break; fi
  sleep 1
done

# With the user networking of QEMU, the host is 10.0.2.2 from inside the unikernel.
key="$(head -c 64 /dev/urandom | base64 | tr -d '\n')"
cat >"${work}/config.json" <<EOF
{
  "Args": ["up", "--cloud", "aws", "--env", "prod", "--demo=false", "--addr", "0.0.0.0:${port}"],
  "Env": {
    "CLANKER_DATABASE_URL": "postgres://postgres:check@10.0.2.2:${address##*:}/clanker?sslmode=disable",
    "CLANKER_SIGNING_KEY": "${key}"
  }
}
EOF

echo "== boot the first unikernel"
boot first

page="$(curl -fs "${base}/" || true)"
script="$(grep -o '/assets/[^"]*\.js' <<<"${page}" | head -n 1 || true)"
if [ -z "${script}" ] || ! curl -fs -o /dev/null "${base}${script}"; then
  fail "the dashboard and its script do not answer"
fi
if [ "$(curl -fs "${base}/api/v1/work-items")" != "[]" ]; then
  fail "the core on a cloud proposed a work item"
fi
if ! curl -fs -o /dev/null "${base}/api/v1/overview"; then fail "the overview does not answer"; fi

echo "== clone a seeded repository and push a branch"
git clone -q "${base}/git/runbooks.git" "${work}/runbooks" 2>>"${work}/git.log"
git -C "${work}/runbooks" fsck --strict --no-dangling >/dev/null || fail "fsck of runbooks"
if [ "$(git -C "${work}/runbooks" rev-parse 'HEAD^{tree}')" != "$(git -C "${repo}" rev-parse 'HEAD:runbooks')" ]; then
  fail "the tree of the runbooks repository is not the tree of runbooks/ in the checkout"
fi
git -C "${work}/runbooks" checkout -q -b nanos-check
echo "written through a unikernel" >"${work}/runbooks/nanos-check.txt"
git -C "${work}/runbooks" add nanos-check.txt
git -C "${work}/runbooks" -c user.name=carol -c user.email=carol@example.com commit -q -m "Check a push"
git -C "${work}/runbooks" push -q origin nanos-check 2>>"${work}/git.log" || fail "the push was refused"
pushed="$(git -C "${work}/runbooks" rev-parse HEAD)"

echo "== stop it and boot a second unikernel on the same database"
stop_unikernel
boot second
found="$(git ls-remote "${base}/git/runbooks.git" refs/heads/nanos-check 2>>"${work}/git.log" | cut -f 1)"
if [ "${found}" != "${pushed}" ]; then
  fail "the second unikernel does not have the pushed branch: want ${pushed}, got ${found:-nothing}"
fi
if [ "$(curl -fs "${base}/api/v1/audit/verify" | grep -c '"ok":true')" != 1 ]; then
  fail "the audit chain does not verify"
fi
stop_unikernel

for log in first second; do
  if grep -q "level=ERROR" "${work}/${log}.log"; then
    fail "error lines in the log of the ${log} unikernel; see ${work}/${log}.log"
  fi
done

if [ "${failures}" -ne 0 ]; then
  echo "RESULT: ${failures} checks failed. The files are in ${work}"
  exit 1
fi
echo "RESULT: the core ran as a unikernel, took a push and kept it across a restart"
