#!/usr/bin/env bash
# Take a logical backup of the Paperboat database, check it, and apply the retention policy.
set -euo pipefail

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
file="/backups/paperboat-${stamp}.dump"

kubectl -n paperboat-data exec postgres-0 -- \
  pg_dump -U postgres -d paperboat --format=custom --file="${file}"
echo "backup written: ${file}"

kubectl -n paperboat-data exec postgres-0 -- sh -c "pg_restore --list '${file}' | wc -l" |
  awk '{ print "objects in backup: " $1; if ($1 < 5) exit 1 }'

retention_days="$(kubectl -n paperboat-data get configmap backup-policy -o 'jsonpath={.data.retention_days}')"
kubectl -n paperboat-data exec postgres-0 -- \
  find /backups -name 'paperboat-*.dump' -mtime "+${retention_days}" -delete
kubectl -n paperboat-data exec postgres-0 -- sh -c 'ls -1 /backups | tail -5'
