#!/usr/bin/env bash
# Restart one Paperboat deployment and wait for the rollout.
# The definition restart-deployment.json declares the input that arrives as PARAM_DEPLOYMENT.
set -euo pipefail

deployment="${PARAM_DEPLOYMENT:?the control plane sets this input}"

kubectl -n paperboat rollout restart "deployment/${deployment}"
kubectl -n paperboat rollout status "deployment/${deployment}" --timeout=180s
kubectl -n paperboat get pods -l "app.kubernetes.io/component=${deployment#paperboat-}" -o wide
