#!/usr/bin/env bash
# Restart one Harbor component and wait for the rollout.
set -euo pipefail

namespace="${TARGET_APPLICATION:?is empty: the control plane sets the namespace of the application}"
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

kubectl -n "${namespace}" rollout restart "deployment/${deployment}"
kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout=180s
