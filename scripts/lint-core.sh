#!/usr/bin/env bash
# The core of the control plane must run as a unikernel, which cannot start a program.
# This check follows the imports of the core packages inside this module and fails when
# one of them imports os/exec. It does not look inside packages of other modules.
#
# Usage: scripts/lint-core.sh <core package>...
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: scripts/lint-core.sh <core package>..." >&2
  exit 2
fi

module="$(go list -m)"
roots=()
for name in "$@"; do
  roots+=("./internal/${name}")
done

offenders="$(go list -deps -f '{{.ImportPath}}{{range .Imports}} {{.}}{{end}}' "${roots[@]}" |
  awk -v module="${module}" 'index($1, module "/") == 1 {
    for (i = 2; i <= NF; i++) if ($i == "os/exec") print $1
  }')"

if [ -n "${offenders}" ]; then
  echo "the core must not start a program, but these packages import os/exec:" >&2
  echo "${offenders}" >&2
  exit 1
fi
