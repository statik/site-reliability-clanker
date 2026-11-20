#!/usr/bin/env bash
# Restart one Paperboat component and wait for the rollout.
# The definition restart-component.json declares the input that arrives as PARAM_COMPONENT.
set -euo pipefail

component="${PARAM_COMPONENT:?the control plane sets this input}"
deployment="paperboat-${component}"

kubectl -n paperboat rollout restart "deployment/${deployment}"
kubectl -n paperboat rollout status "deployment/${deployment}" --timeout=180s
kubectl -n paperboat get pods -l "app.kubernetes.io/component=${component}" -o wide
