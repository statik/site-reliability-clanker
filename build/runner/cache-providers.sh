#!/usr/bin/env bash
# Fill the provider cache of the runner image. It runs `tofu init -backend=false` in a copy
# of each AWS stack with TF_PLUGIN_CACHE_DIR set to the cache, so that a job finds each
# provider there and downloads none.
#
# OpenTofu takes a provider from the cache only when the lock file holds the hash of the
# provider for this platform. When the hash is missing, init writes it into the lock file.
# So a lock file that init changed fails the build.
#
# Usage: cache-providers.sh PLATFORM_DIR CACHE_DIR
set -euo pipefail

source_dir="$1"
export TF_PLUGIN_CACHE_DIR="$2"
mkdir -p "${TF_PLUGIN_CACHE_DIR}"
platform="$(tofu version | sed -n 's/^on //p')"
work="$(mktemp -d)"
cp -R "${source_dir}" "${work}/platform"

failed=0
for stack in "${work}"/platform/stacks/aws/*/; do
  name="${stack#"${work}/"}"
  lock="${stack}.terraform.lock.hcl"
  if [ ! -f "${lock}" ]; then
    echo "${name} has no .terraform.lock.hcl" >&2
    failed=1
    continue
  fi
  cp "${lock}" "${work}/lock.before"
  if ! output="$(tofu -chdir="${stack}" init -backend=false -input=false -no-color 2>&1)"; then
    echo "${output}" >&2
    echo "tofu init failed in ${name}" >&2
    failed=1
    continue
  fi
  if ! cmp -s "${work}/lock.before" "${lock}"; then
    echo "The lock file of ${name} has no hash for ${platform}. Run tofu providers lock" \
      "with -platform=${platform} and each platform that the lock file has now." >&2
    failed=1
  fi
done
rm -rf "${work}"
exit "${failed}"
