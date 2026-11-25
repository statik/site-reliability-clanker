#!/bin/bash
# The entry point of the Keycloak container of the home stack (keycloak.tf). It writes the
# realm in REALM_JSON to the import directory and starts Keycloak in production mode.
#
# Keycloak keeps the text ${NAME} of a realm variable that is not set, for example in the
# client secret. So the script stops, and the deployment fails, when a variable is missing.
#
# Arguments: the names of the variables that the realm reads.
set -euo pipefail

missing=()
for name in "$@"; do
  if [[ -z ${!name:-} ]]; then
    missing+=("${name}")
  fi
done
if ((${#missing[@]} > 0)); then
  echo "keycloak-start: the realm reads these variables, which are not set: ${missing[*]}" >&2
  exit 1
fi

realm="${REALM_JSON:?keycloak-start: REALM_JSON must hold the realm to import}"
mkdir -p /opt/keycloak/data/import
printf '%s' "${realm}" >/opt/keycloak/data/import/realm.json
exec /opt/keycloak/bin/kc.sh start --import-realm
