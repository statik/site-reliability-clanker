#!/usr/bin/env bash
# Sign in to the core through a real Keycloak over plain HTTP, in the shape of a core in a VPC
# that people reach through Tailscale. Keycloak runs in production mode (kc.sh start) with the
# realm of scripts/verify/keycloak/realm.json. The core runs as it runs on a cloud, with its
# own PostgreSQL container.
#
# The core and Keycloak listen on the private address of this machine and not on 127.0.0.1:
# a browser treats a loopback address as a secure context, and Keycloak asks for HTTPS only
# from an address that is not private.
#
# Usage: scripts/verify/keycloak-signin-check.sh
#
# It needs Docker, Go, pnpm and macOS (it finds the private address with ipconfig and route).
# It makes the client secret and the passwords when it starts. While it runs, the core and
# Keycloak answer on the private address. It does not touch the local demo or a kind cluster.
set -euo pipefail

repo="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
version=26.8.2
image="quay.io/keycloak/keycloak:${version}@sha256:7e66a96e4739239070aeaf72e4c5decf0f7f682cfc725b9f7ea2b336eaba38b3"
keycloak="clanker-keycloak-check-$$"
probe="clanker-keycloak-probe-$$"
database="clanker-keycloak-check-db-$$"
port=7270
idp_port=7280
probe_port=7281
core=""
login=""
failures=0

private_address() {
  if ! command -v ipconfig >/dev/null || ! command -v route >/dev/null; then
    echo "this check finds the private address of the machine with ipconfig and route of macOS" >&2
    return 1
  fi
  local found
  found="$(ipconfig getifaddr "$(route -n get default | awk '/interface:/ {print $2}')" || true)"
  case "${found}" in
  10.* | 192.168.* | 172.1[6-9].* | 172.2[0-9].* | 172.3[01].*) echo "${found}" ;;
  *)
    echo "the interface of the default route has no private IPv4 address (found \"${found}\")" >&2
    return 1
    ;;
  esac
}

address="$(private_address)"
base="http://${address}:${port}"
issuer="http://${address}:${idp_port}/realms/clanker"

cleanup() {
  if [ -n "${login}" ]; then kill "${login}" 2>/dev/null || true; fi
  if [ -n "${core}" ]; then kill "${core}" 2>/dev/null || true; fi
  docker logs "${keycloak}" >"${work}/keycloak.log" 2>&1 || true
  docker rm -f "${keycloak}" "${probe}" "${database}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

fail() {
  echo "FAILED: $1"
  failures=$((failures + 1))
}

secret() {
  head -c 24 /dev/urandom | base64 | tr -d '\n/+='
}

# The realm file reads these when Keycloak imports it. The core reads the first three too.
export CLANKER_PUBLIC_URL="${base}"
CLANKER_OIDC_CLIENT_SECRET="$(secret)"
export CLANKER_OIDC_CLIENT_SECRET
export CLANKER_ADMIN_EMAIL=admin@example.com
export REALM_APPROVER_EMAIL=approver@example.com
REALM_ADMIN_PASSWORD="$(secret)"
REALM_APPROVER_PASSWORD="$(secret)"
KC_BOOTSTRAP_ADMIN_PASSWORD="$(secret)"
export REALM_ADMIN_PASSWORD REALM_APPROVER_PASSWORD KC_BOOTSTRAP_ADMIN_PASSWORD

# start_keycloak starts Keycloak in production mode with plain HTTP and imports the realm. $1
# is the name of the container and $2 its port; the other arguments go to kc.sh start. The
# secrets go to Docker by name, so that they are not on a command line.
start_keycloak() {
  local name="$1" listen="$2"
  shift 2
  docker run -d --name "${name}" -p "${address}:${listen}:8080" \
    -e CLANKER_PUBLIC_URL -e CLANKER_OIDC_CLIENT_SECRET -e CLANKER_ADMIN_EMAIL \
    -e REALM_APPROVER_EMAIL -e REALM_ADMIN_PASSWORD -e REALM_APPROVER_PASSWORD \
    -e KC_BOOTSTRAP_ADMIN_USERNAME=bootstrap -e KC_BOOTSTRAP_ADMIN_PASSWORD \
    -v "${repo}/scripts/verify/keycloak/realm.json:/opt/keycloak/data/import/realm.json:ro" \
    "${image}" start --http-enabled=true --hostname="http://${address}:${listen}" \
    --db=dev-file --import-realm "$@" >/dev/null
}

wait_for() {
  for _ in $(seq 1 120); do
    if curl -fs -o /dev/null "$1"; then return 0; fi
    sleep 1
  done
  echo "no answer from $1" >&2
  return 1
}

# core_requests counts the requests of the core (a Go client) in the access log of Keycloak
# whose request line starts with $1.
core_requests() {
  docker logs "${keycloak}" 2>&1 | grep -F "\"$1" | grep -c '"Go-http-client/' || true
}

# expect_from asks the second Keycloak for its login page as a request from the address $1,
# and compares the answer with $2.
expect_from() {
  local answer seen
  answer="$(curl -s -G -H "X-Forwarded-For: $1" --data-urlencode client_id=clanker \
    --data-urlencode response_type=code --data-urlencode scope=openid \
    --data-urlencode code_challenge_method=S256 \
    --data-urlencode code_challenge=check-check-check-check-check-check-check-check \
    --data-urlencode "redirect_uri=${base}/auth/callback" \
    "http://${address}:${probe_port}/realms/clanker/protocol/openid-connect/auth" || true)"
  case "${answer}" in
  *'id="username"'*) seen="the login form" ;;
  *"HTTPS required"*) seen="HTTPS required" ;;
  *) seen="something else" ;;
  esac
  echo "  from $1: ${seen}"
  if [ "${seen}" != "$2" ]; then fail "Keycloak answered a request from $1 with ${seen}, not $2"; fi
}

kcadm() {
  docker exec "${keycloak}" /opt/keycloak/bin/kcadm.sh "$@" --config /tmp/kcadm.config
}

echo "== build the core with its dashboard, on ${address}"
cd "${repo}"
make bundle >"${work}/bundle.log" 2>&1
CGO_ENABLED=0 go build -tags bundle -trimpath -o "${work}/clanker" ./cmd/clanker

echo "== start a database and Keycloak ${version}"
docker run -d --name "${database}" -p 127.0.0.1::5432 -e POSTGRES_PASSWORD=check \
  -e POSTGRES_DB=clanker postgres:18.7 >/dev/null
start_keycloak "${keycloak}" "${idp_port}" --http-access-log-enabled=true \
  --http-access-log-pattern=combined
# A second Keycloak trusts X-Forwarded-For, so that a request can come from any address.
start_keycloak "${probe}" "${probe_port}" --proxy-headers=xforwarded
for _ in $(seq 1 60); do
  if docker exec "${database}" pg_isready -U postgres -d clanker >/dev/null 2>&1; then break; fi
  sleep 1
done
wait_for "${issuer}/.well-known/openid-configuration"
wait_for "http://${address}:${probe_port}/realms/clanker/.well-known/openid-configuration"

echo "== Keycloak asks for HTTPS from an address that is not private, also from a tailnet"
for from in 10.0.0.5 172.16.0.5 192.168.0.5; do expect_from "${from}" "the login form"; done
for from in 100.64.0.1 203.0.113.7; do expect_from "${from}" "HTTPS required"; done
docker rm -f "${probe}" >/dev/null

echo "== start the core with the sign-in of Keycloak"
mkdir "${work}/cwd" "${work}/home" "${work}/client"
before="$(core_requests "GET /realms/clanker/.well-known/openid-configuration")"
address_of_database="$(docker port "${database}" 5432/tcp | head -n 1)"
(
  cd "${work}/cwd"
  unset OPENROUTER_API_KEY
  HOME="${work}/home" CLANKER_OIDC_ISSUER="${issuer}" CLANKER_OIDC_CLIENT_ID=clanker \
    CLANKER_DATABASE_URL="postgres://postgres:check@${address_of_database}/clanker?sslmode=disable" \
    CLANKER_SIGNING_KEY="$(head -c 64 /dev/urandom | base64 | tr -d '\n')" \
    exec "${work}/clanker" up --cloud aws --env prod --demo=false --addr "${address}:${port}"
) >"${work}/up.log" 2>&1 &
core=$!
wait_for "${base}/healthz"
if [ "$(core_requests "GET /realms/clanker/.well-known/openid-configuration")" -le "${before}" ]; then
  fail "the core did not read the discovery document of Keycloak"
else
  echo "  the core read ${issuer}/.well-known/openid-configuration"
fi

echo "== clanker login asks for a code"
HOME="${work}/client" "${work}/clanker" login --server "${base}" >"${work}/login.log" 2>&1 &
login=$!
for _ in $(seq 1 30); do
  if grep -q "Approve only" "${work}/login.log"; then break; fi
  sleep 1
done
page="$(grep -o "${base}/auth/device?user_code=[A-Z-]*" "${work}/login.log" | head -n 1 || true)"
if [ -z "${page}" ]; then fail "clanker login showed no code; see ${work}/login.log"; fi

echo "== in a browser: the admin signs in, approves the code; the approver is refused"
if ! node "${repo}/web/scripts/keycloak-signin-check.mjs" signin "${base}" \
  "${CLANKER_ADMIN_EMAIL}" "${REALM_APPROVER_EMAIL}" "${page}"; then
  fail "the sign-in in the browser"
fi
if wait "${login}"; then
  token="$(HOME="${work}/client" "${work}/clanker" token --server "${base}" || true)"
  me="$(curl -s -H "Authorization: Bearer ${token}" "${base}/api/v1/me")"
  echo "  GET /api/v1/me with the token of clanker login: ${me}"
  if [[ "${me}" != *'"role":"admin"'* ]]; then fail "the token of clanker login does not act as the admin"; fi
else
  fail "clanker login did not end well; see ${work}/login.log"
fi
login=""
for request in "GET /realms/clanker/protocol/openid-connect/certs" \
  "POST /realms/clanker/protocol/openid-connect/token"; do
  if [ "$(core_requests "${request}")" -eq 0 ]; then
    fail "the access log of Keycloak has no request of the core: ${request}"
  else
    echo "  the core sent ${request}"
  fi
done

echo "== the core refuses an email address that Keycloak did not confirm"
KC_CLI_PASSWORD="${KC_BOOTSTRAP_ADMIN_PASSWORD}" docker exec -e KC_CLI_PASSWORD "${keycloak}" \
  /opt/keycloak/bin/kcadm.sh config credentials --config /tmp/kcadm.config \
  --server http://localhost:8080 --realm master --user bootstrap >/dev/null
approver="$(kcadm get users -r clanker -q "email=${REALM_APPROVER_EMAIL}" --fields id --format csv --noquotes)"
kcadm update "users/${approver}" -r clanker -s emailVerified=false
if ! node "${repo}/web/scripts/keycloak-signin-check.mjs" unverified "${base}" \
  "${REALM_APPROVER_EMAIL}"; then
  fail "the sign-in with an email address that is not confirmed"
fi

kill "${core}"
wait "${core}" 2>/dev/null || true
core=""
if grep -q "level=ERROR" "${work}/up.log"; then
  fail "error lines in the log of the core; see ${work}/up.log"
fi

if [ "${failures}" -ne 0 ]; then
  echo "RESULT: ${failures} checks failed. The files are in ${work}"
  exit 1
fi
echo "RESULT: the core signed in a person and a program through Keycloak over plain HTTP on a private address"
