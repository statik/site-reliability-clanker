#!/usr/bin/env bash
# Restore the newest Keycloak backup into a scratch database, compare row counts with
# production, then drop the scratch database.
# The definition restore-drill.json declares the output that this script writes to OUTPUT_FILE.
set -euo pipefail

data="${TARGET_DATA:?is empty: the database of this environment is not in the cluster}"
scratch="keycloak_restore_drill"

count() {
  kubectl -n "${data}" exec postgres-0 -- \
    psql -U postgres -d "$1" -At -c "SELECT count(*) FROM $2"
}

latest="$(kubectl -n "${data}" exec postgres-0 -- sh -c 'ls -1t /backups/keycloak-*.dump 2>/dev/null | head -1')"
if [[ -z "${latest}" ]]; then
  echo "no Keycloak backup found in /backups: run the db-backup runbook first" >&2
  exit 1
fi
echo "restoring ${latest} into scratch database ${scratch}"

kubectl -n "${data}" exec postgres-0 -- dropdb -U postgres --if-exists "${scratch}"
kubectl -n "${data}" exec postgres-0 -- createdb -U postgres "${scratch}"
kubectl -n "${data}" exec postgres-0 -- \
  pg_restore -U postgres -d "${scratch}" --no-owner "${latest}"

# A restored Keycloak has at least the realm master, its administrator and its clients.
for table in realm user_entity client; do
  restored="$(count "${scratch}" "${table}")"
  live="$(count keycloak "${table}")"
  echo "table ${table}: restored ${restored} rows, production has ${live} rows"
  if [[ "${restored}" -eq 0 ]]; then
    echo "restore drill failed: table ${table} is empty after restore" >&2
    exit 1
  fi
done

kubectl -n "${data}" exec postgres-0 -- dropdb -U postgres "${scratch}"
echo "restore drill passed; scratch database dropped"
echo "backup_file=${latest}" >>"${OUTPUT_FILE}"
