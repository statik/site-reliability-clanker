#!/usr/bin/env bash
# Take a logical backup of the Keycloak database, check it, and apply the retention policy.
# The definition db-backup.json declares the outputs that this script writes to OUTPUT_FILE.
set -euo pipefail

data="${TARGET_DATA:?is empty: the database of this environment is not in the cluster}"

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
file="/backups/keycloak-${stamp}.dump"

kubectl -n "${data}" exec postgres-0 -- \
  pg_dump -U postgres -d keycloak --format=custom --file="${file}"
echo "backup written: ${file}"

list="$(kubectl -n "${data}" exec postgres-0 -- pg_restore --list "${file}")"
objects="$(printf '%s\n' "${list}" | wc -l | tr -d '[:space:]')"
echo "objects in backup: ${objects}"
# The tables of the realms and of the users must be in the backup with their rows.
for table in realm user_entity; do
  if ! printf '%s\n' "${list}" | grep -q "TABLE DATA public ${table} "; then
    echo "the backup has no rows of the table ${table}" >&2
    exit 1
  fi
done

retention_days="$(kubectl -n "${data}" get configmap backup-policy -o 'jsonpath={.data.retention_days}')"
kubectl -n "${data}" exec postgres-0 -- \
  find /backups -name 'keycloak-*.dump' -mtime "+${retention_days}" -delete
kubectl -n "${data}" exec postgres-0 -- sh -c 'ls -1 /backups | tail -5'

{
  echo "backup_file=${file}"
  echo "objects=${objects}"
} >>"${OUTPUT_FILE}"
