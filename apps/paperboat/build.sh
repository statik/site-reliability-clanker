#!/usr/bin/env bash
set -euo pipefail

# Builds and pushes every Paperboat release as <registry>/paperboat:<version>, for the
# architecture of this host. Each release is a set of build arguments for one Dockerfile.

if [ "$#" -ne 1 ] || [ -z "$1" ]; then
  echo "usage: build.sh <registry>   for example: build.sh localhost:5001" >&2
  exit 2
fi

registry="${1%/}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Runtime base images, pinned by digest. Release 1.1.1 is the move from the first to the
# second. The value is also what /api/version reports as base_image.
digest_debian12="sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab"
digest_debian13="sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931"
base_debian12="gcr.io/distroless/static-debian12:nonroot@${digest_debian12}"
base_debian13="gcr.io/distroless/static-debian13:nonroot@${digest_debian13}"

build_release() {
  local version="$1" feature_level="$2" fault="$3" base_image="$4"
  local image="${registry}/paperboat:${version}"
  echo "==> ${image} (feature level ${feature_level}, base ${base_image%%@*})"
  docker build \
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
