#!/usr/bin/env bash
# Restart one Harbor deployment and wait for the rollout.
set -euo pipefail

deployment="${PARAM_DEPLOYMENT:?the control plane sets this input}"

kubectl -n harbor rollout restart "deployment/${deployment}"
kubectl -n harbor rollout status "deployment/${deployment}" --timeout=180s
