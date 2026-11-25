#!/usr/bin/env bash
# Routine database upkeep: report dead rows, then vacuum and analyze every table.
set -euo pipefail

data="${TARGET_DATA:?is empty: the database of this environment is not in the cluster}"

psql_prod() {
  kubectl -n "${data}" exec postgres-0 -- psql -U postgres -d paperboat -c "$1"
}

echo "dead rows before maintenance:"
psql_prod "SELECT relname, n_live_tup, n_dead_tup FROM pg_stat_user_tables ORDER BY n_dead_tup DESC LIMIT 8"
psql_prod "VACUUM (ANALYZE)"
echo "dead rows after maintenance:"
psql_prod "SELECT relname, n_live_tup, n_dead_tup FROM pg_stat_user_tables ORDER BY n_dead_tup DESC LIMIT 8"
