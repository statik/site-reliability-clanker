#!/usr/bin/env bash
# Restart one Paperboat deployment and wait for the rollout.
# Parameters: PARAM_DEPLOYMENT (default paperboat-api).
set -euo pipefail

deployment="${PARAM_DEPLOYMENT:-paperboat-api}"

kubectl -n paperboat rollout restart "deployment/${deployment}"
kubectl -n paperboat rollout status "deployment/${deployment}" --timeout=180s
kubectl -n paperboat get pods -l "app.kubernetes.io/component=${deployment#paperboat-}" -o wide
