#!/usr/bin/env bash
set -euo pipefail

# Builds and pushes every Paperboat release as <registry>/paperboat:<version>. Each release is
# a set of build arguments for one Dockerfile. The images are for the architecture of this
# host, or for the platform of --platform, for example linux/arm64 for the Graviton nodes of
# AWS.

usage() {
  echo "usage: build.sh [--platform PLATFORM] <registry>   for example: build.sh localhost:5001" >&2
  exit 2
}

platform=""
if [ "${1:-}" = "--platform" ]; then
  [ "$#" -ge 2 ] && [ -n "$2" ] || usage
  platform="$2"
  shift 2
fi
if [ "$#" -ne 1 ] || [ -z "$1" ]; then
  usage
fi

registry="${1%/}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
platform_args=()
if [ -n "${platform}" ]; then
  platform_args=(--platform "${platform}")
fi

# Runtime base images, pinned by digest. Release 1.1.1 is the move from the first to the
# second. The value is also what /api/version reports as base_image. Each digest is a
# multi-platform index with linux/amd64 and linux/arm64.
digest_debian12="sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab"
digest_debian13="sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931"
base_debian12="gcr.io/distroless/static-debian12:nonroot@${digest_debian12}"
base_debian13="gcr.io/distroless/static-debian13:nonroot@${digest_debian13}"

build_release() {
  local version="$1" feature_level="$2" fault="$3" base_image="$4"
  local image="${registry}/paperboat:${version}"
  echo "==> ${image} (feature level ${feature_level}, base ${base_image%%@*}${platform:+, ${platform}})"
  docker build ${platform_args[@]+"${platform_args[@]}"} \
    --build-arg "VERSION=${version}" \
    --build-arg "FEATURE_LEVEL=${feature_level}" \
    --build-arg "FAULT=${fault}" \
    --build-arg "BASE_IMAGE=${base_image}" \
    --tag "${image}" \
    "${here}"
  docker push "${image}"
}

build_release 1.0.0 1 none "${base_debian12}"
build_release 1.1.0 2 none "${base_debian12}"
build_release 1.1.1 2 none "${base_debian13}"
build_release 1.2.0-bad 2 inventory "${base_debian13}"

echo "Pushed releases 1.0.0, 1.1.0, 1.1.1 and 1.2.0-bad to ${registry}/paperboat"
