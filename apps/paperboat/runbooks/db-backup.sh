#!/usr/bin/env bash
# Take a logical backup of the Paperboat database, check it, and apply the retention policy.
# The definition db-backup.json declares the outputs that this script writes to OUTPUT_FILE.
set -euo pipefail

data="${TARGET_DATA:?is empty: the database of this environment is not in the cluster}"

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
file="/backups/paperboat-${stamp}.dump"

kubectl -n "${data}" exec postgres-0 -- \
  pg_dump -U postgres -d paperboat --format=custom --file="${file}"
echo "backup written: ${file}"

objects="$(kubectl -n "${data}" exec postgres-0 -- \
  sh -c "pg_restore --list '${file}' | wc -l" | tr -d '[:space:]')"
echo "objects in backup: ${objects}"
if [[ "${objects}" -lt 5 ]]; then
  echo "the backup lists fewer than 5 objects" >&2
  exit 1
fi

retention_days="$(kubectl -n "${data}" get configmap backup-policy -o 'jsonpath={.data.retention_days}')"
kubectl -n "${data}" exec postgres-0 -- \
  find /backups -name 'paperboat-*.dump' -mtime "+${retention_days}" -delete
kubectl -n "${data}" exec postgres-0 -- sh -c 'ls -1 /backups | tail -5'

{
  echo "backup_file=${file}"
  echo "objects=${objects}"
} >>"${OUTPUT_FILE}"
