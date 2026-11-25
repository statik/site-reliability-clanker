#!/usr/bin/env bash
# Print a new signing key of the broker for the setting CLANKER_SIGNING_KEY: an Ed25519
# private key in the form of Go, the 32 bytes of the seed and then the 32 bytes of the
# public key, in base64. The checks use it. 64 random bytes are not such a key: the broker
# refuses them, because no signature made with them verifies.
#
# Usage: scripts/verify/signing-key.sh
set -euo pipefail

dir="$(mktemp -d)"
trap 'trash "${dir}"' EXIT
openssl genpkey -algorithm ed25519 -out "${dir}/key.pem"
{
  openssl pkey -in "${dir}/key.pem" -outform DER | tail -c 32
  openssl pkey -in "${dir}/key.pem" -pubout -outform DER | tail -c 32
} | base64 | tr -d '\n'
