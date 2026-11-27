#!/usr/bin/env bash
# Restart one component of the Keycloak pack and wait for the rollout.
# The definition restart-component.json declares the input that arrives as PARAM_COMPONENT.
set -euo pipefail

namespace="${TARGET_APPLICATION:?is empty: the control plane sets the namespace of the application}"
component="${PARAM_COMPONENT:?the control plane sets this input}"
case "${component}" in
server) deployment="keycloak" ;;
*)
  echo "the pack has no component ${component}" >&2
  exit 2
  ;;
esac

# The deployment has the strategy Recreate: the old pods stop, then the new ones start and
# become ready when Keycloak has started. A start that migrates nothing took 25 s on kind; a
# start that migrates the database can take much longer.
kubectl -n "${namespace}" rollout restart "deployment/${deployment}"
kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout=600s
kubectl -n "${namespace}" get pods -l "app.kubernetes.io/name=keycloak" -o wide
