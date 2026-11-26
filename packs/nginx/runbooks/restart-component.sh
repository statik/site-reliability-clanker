#!/usr/bin/env bash
# Restart one component of the nginx pack and wait for the rollout.
# The definition restart-component.json declares the input that arrives as PARAM_COMPONENT.
set -euo pipefail

namespace="${TARGET_APPLICATION:?is empty: the control plane sets the namespace of the application}"
component="${PARAM_COMPONENT:?the control plane sets this input}"
case "${component}" in
server) deployment="nginx" ;;
*)
  echo "the pack has no component ${component}" >&2
  exit 2
  ;;
esac

kubectl -n "${namespace}" rollout restart "deployment/${deployment}"
kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout=180s
kubectl -n "${namespace}" get pods -l "app.kubernetes.io/name=nginx" -o wide
