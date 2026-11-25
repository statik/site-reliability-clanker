#!/usr/bin/env bash
# Run `clanker bootstrap --cloud aws` and `clanker destroy --cloud aws` against a MinIO
# container that stands in for S3. The source files are the fixture in
# scripts/verify/awsfixture: the six AWS layers with terraform_data resources and an s3
# backend. A stub of the aws program answers sts and ssm and gives every other call to the
# AWS CLI, which talks to MinIO. Nothing reaches AWS: the check removes the AWS variables of
# the shell, the AWS CLI reads temporary configuration files, and every endpoint is MinIO.
#
# Then the local target: two bootstraps with two data directories from this checkout, which
# must stay as it is (#140), and a local destroy of one of them. They touch no kind cluster.
#
# Usage: scripts/verify/aws-bootstrap-destroy-check.sh
#
# It needs Docker, Go, OpenTofu, the AWS CLI v2 and jq.
set -euo pipefail

repo="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
minio="clanker-aws-bootstrap-check-$$"
# The same frozen MinIO build as runner-image-check.sh.
minio_image="bitnamilegacy/minio:2025.7.23-debian-12-r5"
minio_image="${minio_image}@sha256:6dabb4a2088c9a79908de3bc05f4586c23ad2182c8908e7e3acbf61c1467fb20"
env_name="check-env"
account="000000000000"
bucket="clanker-${env_name}-state-${account}"
access_key="aws-bootstrap-check"
secret_key="aws-bootstrap-check-secret-$$"
failures=0

cleanup() {
  docker rm -f "${minio}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

fail() {
  echo "FAILED: $1"
  failures=$((failures + 1))
}

ok() {
  echo "ok: $1"
}

# expect runs a command and reports the message as a passed or a failed check.
expect() {
  local message="$1"
  shift
  if "$@"; then
    ok "${message}"
  else
    fail "${message}"
  fi
}

# not runs a command and succeeds when it fails.
not() {
  ! "$@"
}

for name in $(env | sed -n 's/^\(AWS_[A-Z0-9_]*\)=.*/\1/p'); do
  unset "${name}"
done
real_aws="$(command -v aws)"

echo "== MinIO with a KMS key, so that the backend setting encrypt=true works"
kms_key="check-key:$(head -c 32 /dev/urandom | base64)"
docker run -d --name "${minio}" -p 127.0.0.1::9000 \
  --env MINIO_ROOT_USER="${access_key}" --env MINIO_ROOT_PASSWORD="${secret_key}" \
  --env MINIO_KMS_SECRET_KEY="${kms_key}" "${minio_image}" >/dev/null
port="$(docker port "${minio}" 9000 | head -1 | sed 's/.*://')"

mkdir -p "${work}/aws" "${work}/bin" "${work}/record"
printf '[profile check]\nregion = us-east-1\n' >"${work}/aws/config"
printf '[check]\naws_access_key_id = %s\naws_secret_access_key = %s\n' \
  "${access_key}" "${secret_key}" >"${work}/aws/credentials"
export AWS_CONFIG_FILE="${work}/aws/config"
export AWS_SHARED_CREDENTIALS_FILE="${work}/aws/credentials"
export AWS_ENDPOINT_URL="http://127.0.0.1:${port}"
export AWS_ENDPOINT_URL_S3="http://127.0.0.1:${port}"
export AWS_EC2_METADATA_DISABLED=true
export REAL_AWS="${real_aws}" STUB_RECORD="${work}/record"

cat >"${work}/bin/aws" <<'STUB'
#!/usr/bin/env bash
# The aws program of the check: sts and ssm answer here, the rest goes to MinIO.
set -euo pipefail
case "$1 $2" in
"sts get-caller-identity")
  echo '{"Account":"000000000000","Arn":"arn:aws:iam::000000000000:user/check"}'
  ;;
"ssm put-parameter")
  printf '%s\n' "$@" >"${STUB_RECORD}/put-arguments"
  value=""
  while [ "$#" -gt 0 ]; do
    if [ "$1" = --value ]; then value="$2"; fi
    shift
  done
  cp "${value#file://}" "${STUB_RECORD}/put-value"
  echo '{"Version":1,"Tier":"Standard"}'
  ;;
*) exec "${REAL_AWS}" "$@" ;;
esac
STUB
chmod +x "${work}/bin/aws"
export PATH="${work}/bin:${PATH}"

s3() {
  "${real_aws}" --profile check "$@"
}

for attempt in $(seq 1 30); do
  if s3 s3api create-bucket --bucket "${bucket}" >/dev/null 2>&1; then
    break
  fi
  if [ "${attempt}" = 30 ]; then
    echo "FAILED: MinIO did not take the bucket in 30 attempts"
    docker logs "${minio}" 2>&1 | tail -20
    exit 1
  fi
  sleep 1
done

# resources prints the number of resources in the state of a layer in the bucket, or
# "none" when the bucket has no state for it.
resources() {
  local key="${env_name}/$1.tfstate"
  if ! s3 s3api head-object --bucket "${bucket}" --key "${key}" >/dev/null 2>&1; then
    echo none
    return
  fi
  s3 s3 cp "s3://${bucket}/${key}" - | jq '.resources | length'
}

echo "== build clanker and copy the fixture"
(cd "${repo}" && go build -o "${work}/clanker" ./cmd/clanker)
cp -R "${repo}/scripts/verify/awsfixture" "${work}/source"
source_files="$(cd "${work}/source" && find . | sort)"
local_state="${work}/data/state/${env_name}/00-bootstrap.tfstate"

# clanker_aws runs clanker as an owner does, with the keys of another session in the shell.
# MinIO knows only the keys of the profile, so each call proves that the profile wins.
clanker_aws() {
  AWS_ACCESS_KEY_ID=key-of-the-shell AWS_SECRET_ACCESS_KEY=secret-of-the-shell \
    AWS_SESSION_TOKEN=token-of-the-shell "${work}/clanker" "$@"
}
common=(--cloud aws --env "${env_name}" --profile check --region us-east-1
  --data "${work}/data" --source "${work}/source" --pack "${repo}/apps/paperboat"
  --broker-principal "arn:aws:iam::${account}:role/check-core")

echo "== bootstrap: local apply, move into the bucket, the setting"
if ! clanker_aws bootstrap "${common[@]}" --parameter-path /src/check \
  --kms-key alias/check >"${work}/bootstrap-1.log" 2>&1; then
  cat "${work}/bootstrap-1.log"
  fail "the first bootstrap failed"
fi
expect "the first bootstrap applied with a local state and moved it into the bucket" \
  grep -q "Moving the state of 00-bootstrap into the bucket ${bucket}" "${work}/bootstrap-1.log"
count="$(resources 00-bootstrap)"
expect "the bucket holds the state of 00-bootstrap with resources (${count})" \
  test "${count}" != none -a "${count}" != 0
encryption="$(s3 s3api head-object --bucket "${bucket}" \
  --key "${env_name}/00-bootstrap.tfstate" --query ServerSideEncryption --output text)"
expect "the state object is encrypted (${encryption})" test "${encryption}" = AES256
path_var="$(s3 s3 cp "s3://${bucket}/${env_name}/00-bootstrap.tfstate" - | jq -r \
  '.resources[] | select(.name == "identities") | .instances[0].attributes.input' |
  jq -r '.value.core_parameter_path')"
expect "-var gave core_parameter_path (${path_var})" test "${path_var}" = /src/check
expect "no local state file is left" test ! -e "${local_state}"
keys="$(s3 s3api list-objects-v2 --bucket "${bucket}" --query 'Contents[].Key' --output text)"
expect "the bucket holds only the state, no lock (${keys})" \
  test "${keys}" = "${env_name}/00-bootstrap.tfstate"

printed="$(sed -n '/It holds no secret:/{n;p;}' "${work}/bootstrap-1.log")"
put_value="$(cat "${work}/record/put-value" 2>/dev/null || true)"
expect "the printed setting is the written one" \
  test -n "${printed}" -a "${printed}" = "${put_value}"
names="$(echo "${put_value}" | jq -r 'keys | join(",")')"
want_names="clanker_ttl,evidence_bucket,identities,permission_guard,state_backend"
want_names="${want_names},workload_role_path"
expect "the setting has the outputs of the layer (${names})" test "${names}" = "${want_names}"
expect "each output of the setting is as tofu output -json prints it" grep -qx true \
  <<<"$(jq 'all(.[]; .sensitive == false and has("type") and has("value"))' <<<"${put_value}")"
expect "the value of the sensitive output was neither printed nor written" \
  not grep -q "fixture-secret-value" "${work}/bootstrap-1.log" "${work}/record/put-value"
arguments="$(tr '\n' ' ' <"${work}/record/put-arguments")"
want_put="--name /src/check/CLANKER_AWS_BOOTSTRAP --type SecureString --key-id alias/check"
expect "put-parameter wrote a SecureString with the key: ${arguments}" \
  grep -q -- "${want_put} .*--overwrite" <<<"${arguments}"
expect "the bootstrap left no file in the source files" \
  test "$(cd "${work}/source" && find . | sort)" = "${source_files}"

echo "== a second bootstrap applies with the state in the bucket"
if ! clanker_aws bootstrap "${common[@]}" --parameter-path /src/check \
  >"${work}/bootstrap-2.log" 2>&1; then
  cat "${work}/bootstrap-2.log"
  fail "the second bootstrap failed"
fi
expect "the second bootstrap applied with the state in the bucket" grep -q \
  "holds the state of ${env_name}/00-bootstrap. Applying the layer with it" \
  "${work}/bootstrap-2.log"
expect "the second bootstrap moved no state" \
  not grep -q "Moving the state" "${work}/bootstrap-2.log"

echo "== the layers that the core would apply: 10-network and 30-data; 40-platform is empty"
apply_layer() {
  local layer="$1" action="$2"
  TF_DATA_DIR="${work}/core/${layer}" tofu -chdir="${work}/source/platform/stacks/aws/${layer}" \
    init -input=false -no-color -backend-config="bucket=${bucket}" \
    -backend-config="key=${env_name}/${layer}.tfstate" -backend-config=region=us-east-1 \
    -backend-config=use_lockfile=true -backend-config=encrypt=true >/dev/null
  TF_DATA_DIR="${work}/core/${layer}" TF_VAR_image_registry=registry.check/paperboat \
    tofu -chdir="${work}/source/platform/stacks/aws/${layer}" "${action}" -input=false \
    -no-color -auto-approve -var "env_name=${env_name}" -var "state_location=${bucket}" \
    -var region=us-east-1 >/dev/null
}
export AWS_PROFILE=check
apply_layer 10-network apply
apply_layer 30-data apply
apply_layer 40-platform apply
apply_layer 40-platform destroy
unset AWS_PROFILE
echo "resources: 10-network $(resources 10-network), 30-data $(resources 30-data)," \
  "40-platform $(resources 40-platform), 20-cluster $(resources 20-cluster)," \
  "50-app $(resources 50-app)"

echo "== destroy with a state that cannot be read: it fails and destroys nothing"
printf 'not a state' >"${work}/unreadable"
s3 s3 cp "${work}/unreadable" "s3://${bucket}/${env_name}/20-cluster.tfstate" >/dev/null
if clanker_aws destroy "${common[@]}" >"${work}/destroy-1.log" 2>&1; then
  fail "the destroy passed with a state that cannot be read"
fi
named="$(grep -o 'check layer 20-cluster[^:]*' "${work}/destroy-1.log" | head -1 || true)"
expect "the destroy named the layer: ${named}" \
  grep -q "check layer 20-cluster of ${env_name}" "${work}/destroy-1.log"
expect "the failed destroy printed no success" not grep -q "is destroyed" "${work}/destroy-1.log"
expect "nothing was destroyed and 00-bootstrap stayed in the bucket" test \
  "$(resources 10-network) $(resources 30-data) $(resources 00-bootstrap)" = "1 1 ${count}" \
  -a ! -e "${local_state}"
s3 s3 rm "s3://${bucket}/${env_name}/20-cluster.tfstate" >/dev/null

echo "== destroy without TF_VAR_image_registry: 30-data goes, 10-network stops it"
if clanker_aws destroy "${common[@]}" >"${work}/destroy-2.log" 2>&1; then
  fail "the destroy passed without the input image_registry of 10-network"
fi
expect "the destroy stopped at 10-network for image_registry" \
  grep -q "10-network.*image_registry\|image_registry.*10-network" \
  <<<"$(tr '\n' ' ' <"${work}/destroy-2.log")"
expect "30-data was destroyed and 10-network was not" \
  test "$(resources 30-data) $(resources 10-network)" = "0 1"
expect "the stopped destroy printed no success" not grep -q "is destroyed" "${work}/destroy-2.log"

echo "== destroy again with TF_VAR_image_registry"
export TF_VAR_image_registry=registry.check/paperboat
if ! clanker_aws destroy "${common[@]}" >"${work}/destroy-3.log" 2>&1; then
  cat "${work}/destroy-3.log"
  fail "the destroy failed"
fi
sed -n '/^Resources in the state/,/00-bootstrap/p' "${work}/destroy-3.log"
for layer in 50-app 40-platform 30-data 20-cluster; do
  expect "the destroy skipped ${layer}, which has no resources" \
    not grep -q "^Destroying ${env_name}/${layer}" "${work}/destroy-3.log"
done
expect "the destroy destroyed 10-network" \
  grep -q "^Destroying ${env_name}/10-network" "${work}/destroy-3.log"
expect "the destroy moved 00-bootstrap out of the bucket" \
  grep -q "Moving the state of 00-bootstrap from the bucket ${bucket}" "${work}/destroy-3.log"
expect "the destroy destroyed 00-bootstrap" \
  grep -q "^Destroying ${env_name}/00-bootstrap" "${work}/destroy-3.log"
expect "the destroy printed success" \
  grep -q "Environment ${env_name} is destroyed" "${work}/destroy-3.log"
expect "the states of 10-network and 30-data are empty" \
  test "$(resources 10-network) $(resources 30-data)" = "0 0"
expect "no local state of 00-bootstrap is left" test ! -e "${local_state}"
keys="$(s3 s3api list-objects-v2 --bucket "${bucket}" --query 'Contents[].Key' --output text)"
expect "no lock is left in the bucket (${keys})" not grep -q tflock <<<"${keys}"

echo "== the local target: two data directories from one checkout (#140)"
stack="${repo}/platform/stacks/local/00-bootstrap"
local_files="$(cd "${stack}" && find . | sort)"
for data in local-a local-b; do
  if ! "${work}/clanker" bootstrap --cloud local --data "${work}/${data}" --source "${repo}" \
    >"${work}/${data}.log" 2>&1; then
    tail -20 "${work}/${data}.log"
    fail "the local bootstrap with the data directory ${data} failed"
  fi
  expect "the local bootstrap of ${data} wrote its state below its data directory" \
    test -s "${work}/${data}/state/local/00-bootstrap.tfstate"
done
expect "the stack of the checkout has no new file" \
  test "$(cd "${stack}" && find . | sort)" = "${local_files}"
if ! "${work}/clanker" destroy --env local --data "${work}/local-a" --source "${repo}" \
  >"${work}/local-destroy.log" 2>&1; then
  cat "${work}/local-destroy.log"
  fail "the local destroy failed"
fi
expect "the local destroy destroyed 00-bootstrap and removed its state file" \
  grep -q "^Destroying local/00-bootstrap" "${work}/local-destroy.log"
expect "the local state file of 00-bootstrap is gone" \
  test ! -e "${work}/local-a/state/local/00-bootstrap.tfstate"

if [ "${failures}" -gt 0 ]; then
  echo "${failures} checks failed; the logs are in ${work}"
  exit 1
fi
echo "RESULT: bootstrap moved 00-bootstrap into the bucket and wrote the setting, destroy" \
  "checked every layer first, skipped the empty ones and removed the rest; the local" \
  "target kept the checkout clean"
