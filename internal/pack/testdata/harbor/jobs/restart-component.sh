#!/usr/bin/env bash
# Restart one Harbor component and wait for the rollout.
set -euo pipefail

component="${PARAM_COMPONENT:?the control plane sets this input}"

case "${component}" in
api) deployment="harbor-gateway" ;;
web) deployment="harbor-ui" ;;
worker) deployment="harbor-jobs" ;;
*)
  echo "the component ${component} is not a component of Harbor" >&2
  exit 1
  ;;
esac

kubectl -n harbor rollout restart "deployment/${deployment}"
kubectl -n harbor rollout status "deployment/${deployment}" --timeout=180s
