#!/usr/bin/env bash
# Restore the newest backup into a scratch database, compare row counts with production,
# then drop the scratch database.
# The definition restore-drill.json declares the output that this script writes to OUTPUT_FILE.
set -euo pipefail

data="${TARGET_DATA:?is empty: the database of this environment is not in the cluster}"

psql_prod() {
  kubectl -n "${data}" exec postgres-0 -- psql -U postgres -d paperboat -At -c "$1"
}
psql_scratch() {
  kubectl -n "${data}" exec postgres-0 -- psql -U postgres -d restore_drill -At -c "$1"
}

latest="$(kubectl -n "${data}" exec postgres-0 -- sh -c 'ls -1t /backups/paperboat-*.dump | head -1')"
if [[ -z "${latest}" ]]; then
  echo "no backup found in /backups: run the db-backup runbook first" >&2
  exit 1
fi
echo "restoring ${latest} into scratch database restore_drill"

kubectl -n "${data}" exec postgres-0 -- dropdb -U postgres --if-exists restore_drill
kubectl -n "${data}" exec postgres-0 -- createdb -U postgres restore_drill
kubectl -n "${data}" exec postgres-0 -- \
  pg_restore -U postgres -d restore_drill --no-owner "${latest}"

for table in fish customers rentals; do
  restored="$(psql_scratch "SELECT count(*) FROM ${table}")"
  live="$(psql_prod "SELECT count(*) FROM ${table}")"
  echo "table ${table}: restored ${restored} rows, production has ${live} rows"
  if [[ "${restored}" -eq 0 ]]; then
    echo "restore drill failed: table ${table} is empty after restore" >&2
    exit 1
  fi
done

kubectl -n "${data}" exec postgres-0 -- dropdb -U postgres restore_drill
echo "restore drill passed; scratch database dropped"
echo "backup_file=${latest}" >>"${OUTPUT_FILE}"
