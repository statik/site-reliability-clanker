#!/usr/bin/env bash
# Wait until the load balancer of a Service has an address in the status of the Service.
# The local-exec provisioner of this module runs it in a job of the runner, with the keys of
# the job in the environment. kubectl gets its token from `aws eks get-token`.
#
# Environment: CLUSTER_NAME, CLUSTER_ENDPOINT, CLUSTER_CA_DATA (PEM in base64), REGION,
# NAMESPACE, SERVICE.
set -euo pipefail

timeout_seconds=600
interval_seconds=10

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
config="${work}/kubeconfig"
printf '%s' "${CLUSTER_CA_DATA}" | base64 -d >"${work}/ca.pem"
kubectl config --kubeconfig "${config}" set-cluster eks --server "${CLUSTER_ENDPOINT}" \
  --certificate-authority "${work}/ca.pem" --embed-certs >/dev/null
kubectl config --kubeconfig "${config}" set-credentials job \
  --exec-api-version client.authentication.k8s.io/v1beta1 --exec-command aws \
  --exec-arg "eks,get-token,--cluster-name,${CLUSTER_NAME},--region,${REGION},--output,json" \
  --exec-interactive-mode Never >/dev/null
kubectl config --kubeconfig "${config}" set-context job --cluster eks --user job >/dev/null
kubectl config --kubeconfig "${config}" use-context job >/dev/null

echo "Waiting up to ${timeout_seconds} s for the load balancer address of ${NAMESPACE}/${SERVICE}"
deadline=$((SECONDS + timeout_seconds))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  if address="$(kubectl --kubeconfig "${config}" --request-timeout 20s -n "${NAMESPACE}" \
    get service "${SERVICE}" --ignore-not-found \
    -o jsonpath='{.status.loadBalancer.ingress[0].hostname}{.status.loadBalancer.ingress[0].ip}' \
    2>"${work}/error")"; then
    if [ -n "${address}" ]; then
      echo "${NAMESPACE}/${SERVICE} has the address ${address}"
      exit 0
    fi
  else
    echo "Reading ${NAMESPACE}/${SERVICE} failed, trying again: $(cat "${work}/error")"
  fi
  sleep "${interval_seconds}"
done

echo "${NAMESPACE}/${SERVICE} has no load balancer address after ${timeout_seconds} s." \
  "The Service and its events:" >&2
kubectl --kubeconfig "${config}" -n "${NAMESPACE}" get service "${SERVICE}" -o wide >&2 || true
kubectl --kubeconfig "${config}" -n "${NAMESPACE}" get events \
  --field-selector "involvedObject.name=${SERVICE}" >&2 || true
exit 1
