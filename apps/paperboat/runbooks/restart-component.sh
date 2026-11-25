#!/usr/bin/env bash
# Restart one Paperboat component and wait for the rollout.
# The definition restart-component.json declares the input that arrives as PARAM_COMPONENT.
set -euo pipefail

namespace="${TARGET_APPLICATION:?is empty: the control plane sets the namespace of the application}"
component="${PARAM_COMPONENT:?the control plane sets this input}"
deployment="paperboat-${component}"

kubectl -n "${namespace}" rollout restart "deployment/${deployment}"
kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout=180s
kubectl -n "${namespace}" get pods -l "app.kubernetes.io/component=${component}" -o wide
