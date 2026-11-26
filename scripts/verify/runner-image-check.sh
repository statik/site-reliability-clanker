#!/usr/bin/env bash
# Build the runner image and run jobs in it as a launcher on a cloud does: each job in a new
# container, with its state in an S3 bucket. The bucket is in a MinIO container. A Go
# harness (scripts/verify/runnerharness) serves the runner routes of the dispatcher and
# runs a plan, an apply with a changed plan file and the apply of the plan. Then it stops a
# runner container with `docker stop` in the middle of an apply, as ECS stops a task, and
# the runner must interrupt OpenTofu so that it saves its state and releases its lock.
# Then the check reads the state from the bucket with the AWS CLI of the image.
#
# Usage: scripts/verify/runner-image-check.sh
#
# It needs Docker Desktop, because a container reaches the harness on the host as
# host.docker.internal. It also needs Go, pnpm and git. It makes its own image tag, network
# and MinIO container and removes them at the end. It does not touch the local demo.
set -euo pipefail

repo="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
image="clanker-runner:check-$$"
network="clanker-runner-check-$$"
minio="clanker-runner-check-minio-$$"
# MinIO publishes no more images. This is the last MinIO build of Bitnami, which is frozen.
# The check uses it only as a stand-in for S3.
minio_image="bitnamilegacy/minio:2025.7.23-debian-12-r5"
minio_image="${minio_image}@sha256:6dabb4a2088c9a79908de3bc05f4586c23ad2182c8908e7e3acbf61c1467fb20"
bucket="state"
access_key="runner-check"
secret_key="runner-check-secret-$$"
message="applied by the runner image check $$"
data="/var/lib/clanker-runner"
failures=0

cleanup() {
  docker rm -f "${minio}" >/dev/null 2>&1 || true
  docker network rm "${network}" >/dev/null 2>&1 || true
  docker image rm "${image}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

fail() {
  echo "FAILED: $1"
  failures=$((failures + 1))
}

# in_image runs a program of the image instead of the runner. The other arguments go to it.
in_image() {
  local program="$1"
  shift
  docker run --rm --platform linux/arm64 --entrypoint "${program}" "${image}" "$@"
}

# aws_cli runs the AWS CLI of the image against the MinIO container.
aws_cli() {
  docker run --rm --platform linux/arm64 --network "${network}" \
    --env AWS_ACCESS_KEY_ID="${access_key}" --env AWS_SECRET_ACCESS_KEY="${secret_key}" \
    --env AWS_DEFAULT_REGION=us-east-1 --env AWS_ENDPOINT_URL=http://minio:9000 \
    --entrypoint aws "${image}" "$@"
}

echo "== build the image ${image}"
make -C "${repo}" runner-image RUNNER_IMAGE="${image}"

echo "== the contents of the image"
arch="$(docker image inspect --format '{{.Architecture}}' "${image}")"
[ "${arch}" = "arm64" ] || fail "the image is for ${arch}, not arm64"
uid="$(in_image id -u)"
[ "${uid}" != "0" ] || fail "the image runs as root"
entrypoint="$(docker image inspect --format '{{json .Config.Entrypoint}}' "${image}")"
want_entrypoint='["clanker","runner","--data","'"${data}"'"]'
[ "${entrypoint}" = "${want_entrypoint}" ] || fail "the entrypoint is ${entrypoint}"
pin="$(sed -n 's/^ *tofu_version: *//p' "${repo}/.github/workflows/ci.yaml")"
tofu_version="$(in_image tofu version | head -1)"
[ "${tofu_version}" = "OpenTofu v${pin}" ] ||
  fail "the image has ${tofu_version}, CI pins OpenTofu ${pin}"
echo "uid ${uid}, ${arch}, entrypoint ${entrypoint}"
echo "clanker $(in_image clanker version)"
echo "${tofu_version}"
in_image kubectl version --client | head -1
echo "helm $(in_image helm version --short)"
in_image aws --version
in_image git --version
if out="$(docker run --rm --platform linux/arm64 "${image}" 2>&1)"; then
  fail "the runner started with no core and no token"
elif [[ "${out}" != *"CLANKER_CORE_URL"* ]]; then
  fail "the runner with no settings printed: ${out}"
fi

echo "== the provider cache serves each AWS stack with no network"
if ! tar -C "${repo}" --exclude .terraform -cf - platform |
  docker run -i --rm --platform linux/arm64 --network none --entrypoint bash "${image}" -c '
    set -euo pipefail
    mkdir /tmp/source
    tar -x -C /tmp/source
    for stack in /tmp/source/platform/stacks/aws/*/; do
      TF_PLUGIN_CACHE_DIR='"${data}"'/plugin-cache \
        tofu -chdir="${stack}" init -backend=false -input=false -no-color >/dev/null
      echo "${stack#/tmp/source/}: init with no network"
    done'; then
  fail "tofu init needs the network for a stack: the provider cache misses a provider"
fi

# The digest that runner.ToolDigest computes from the tools of the image.
sum="$(in_image bash -c 'set -e; tofu version; helm version --short; kubectl version --client' |
  shasum -a 256 | cut -c1-32)"
digest="tools-sha256:${sum}"
echo "tool digest of the image: ${digest}"

echo "== MinIO with the bucket ${bucket}"
docker network create "${network}" >/dev/null
docker run -d --name "${minio}" --network "${network}" --network-alias minio \
  --env MINIO_ROOT_USER="${access_key}" --env MINIO_ROOT_PASSWORD="${secret_key}" \
  "${minio_image}" >/dev/null
for attempt in $(seq 1 30); do
  if aws_cli s3api create-bucket --bucket "${bucket}" >/dev/null 2>&1; then
    break
  fi
  if [ "${attempt}" = 30 ]; then
    echo "FAILED: MinIO did not take the bucket in 30 attempts"
    docker logs "${minio}" 2>&1 | tail -20
    exit 1
  fi
  sleep 1
done

echo "== the jobs, each in its own container"
(cd "${repo}" && go build -o "${work}/runnerharness" ./scripts/verify/runnerharness)
if ! "${work}/runnerharness" --image "${image}" --network "${network}" --digest "${digest}" \
  --message "${message}" --bucket "${bucket}" --s3-access-key "${access_key}" \
  --s3-secret-key "${secret_key}"; then
  fail "a job check failed (see above)"
fi

echo "== the state in the bucket"
keys="$(aws_cli s3api list-objects-v2 --bucket "${bucket}" --query 'Contents[].Key' \
  --output text)"
echo "objects: ${keys}"
[ "$(echo "${keys}" | tr -s '[:space:]' ' ')" = "check/10-state.tfstate check/20-stop.tfstate " ] ||
  fail "the bucket must hold only the two states, and no lock: ${keys}"
output="$(aws_cli s3 cp "s3://${bucket}/check/10-state.tfstate" - | jq -r '.outputs.message.value')"
[ "${output}" = "${message}" ] ||
  fail "the state has the output message \"${output}\", want \"${message}\""

if [ "${failures}" -gt 0 ]; then
  echo "${failures} checks failed"
  exit 1
fi
echo "RESULT: the runner image planned and applied in separate containers with S3 state," \
  "refused a changed plan, and left no lock also when it was stopped in an apply"
