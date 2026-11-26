#!/usr/bin/env bash
# Run the core in AWS mode against stand-ins for AWS on this machine, and let it deploy the
# first environment. No call goes to AWS.
#
# - scripts/verify/fakeaws serves Parameter Store, STS and ECS. Its RunTask runs the runner
#   image with Docker, as Fargate runs a runner task. Its AssumeRole gets a session of
#   MinIO, so each job gets a session with a session token, as from STS.
# - MinIO holds the state of the layers, with server-side encryption.
# - The stacks of layers 10 to 50 are the stand-in stacks of scripts/verify/fakeaws/fixture.
#   They use no provider. 10-network has no resource, like the real layer in its
#   existing-network mode.
# - The core gets every setting from the stand-in for Parameter Store. Its environment
#   holds only the path, the region and the address of the stand-in, and it starts with
#   --fake-aws.
#
# The check starts the core once without the runner setting, which must stand by, and then
# with every setting. The first deployment must plan 10-network in a runner task and wait
# for approval. The admin and a second approver approve it through the API, and every layer
# must be applied in runner tasks with STS sessions and state in the bucket.
#
# Usage: scripts/verify/aws-core-check.sh
#
# It needs Docker Desktop, because a runner container reaches the core on this machine as
# host.docker.internal. It builds the runner image from the commit, unless RUNNER_IMAGE
# names an image that exists. It makes its own containers, network and image, and removes
# them at the end. It does not touch the local demo or kind.
set -euo pipefail

repo="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
port=7270
base="http://localhost:${port}"
idp_port=7271
idp="http://127.0.0.1:${idp_port}"
aws_port=7272
aws="http://127.0.0.1:${aws_port}"
network="clanker-aws-check-$$"
minio="clanker-aws-check-minio-$$"
database="clanker-aws-check-db-$$"
# MinIO publishes no more images. This is the last MinIO build of Bitnami, which is frozen.
minio_image="bitnamilegacy/minio:2025.7.23-debian-12-r5"
minio_image="${minio_image}@sha256:6dabb4a2088c9a79908de3bc05f4586c23ad2182c8908e7e3acbf61c1467fb20"
minio_key="aws-check"
minio_secret="aws-check-secret-$$"
bucket="state"
region="us-east-1"
env_name="check"
image="${RUNNER_IMAGE:-}"
own_image=""
# The values of the account are made up. The check looks for each one in the log of the core.
network_id="vpc-0a1b2c3d4e5f60718"
team_tag="check-team-value-$$"
core=""
provider=""
standin=""
failures=0

cleanup() {
  if [ -n "${core}" ]; then kill "${core}" 2>/dev/null || true; fi
  if [ -n "${provider}" ]; then kill "${provider}" 2>/dev/null || true; fi
  if [ -n "${standin}" ]; then
    kill "${standin}" 2>/dev/null || true
    wait "${standin}" 2>/dev/null || true
  fi
  docker rm -f "${database}" "${minio}" >/dev/null 2>&1 || true
  docker network rm "${network}" >/dev/null 2>&1 || true
  if [ -n "${own_image}" ]; then docker image rm "${own_image}" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

fail() {
  echo "FAILED: $1"
  failures=$((failures + 1))
}

status() {
  curl -s -o /dev/null -w '%{http_code}' "$@"
}

sql() {
  docker exec "${database}" psql -U postgres -d clanker -At -c "$1"
}

# aws_cli runs the AWS CLI of the runner image against MinIO, as the MinIO user.
aws_cli() {
  docker run --rm --platform linux/arm64 --network "${network}" \
    --env AWS_ACCESS_KEY_ID="${minio_key}" --env AWS_SECRET_ACCESS_KEY="${minio_secret}" \
    --env AWS_DEFAULT_REGION="${region}" --env AWS_ENDPOINT_URL=http://minio:9000 \
    --entrypoint aws "${image}" "$@"
}

# sign_in signs the cookie jar $2 in as $1 and prints the role of the signed-in user. The
# source files of the check have no built dashboard, so the sign-in ends on a 404 page.
sign_in() {
  curl -fs -X POST "${idp}/person?email=$1&verified=true"
  curl -s -o /dev/null -L -c "$2" -b "$2" "${base}/auth/login"
  curl -s -b "$2" "${base}/api/v1/me" | jq -r .role
}

# start_core starts the core with the parameter path $1 and the log file $2.
start_core() {
  env -i PATH=/usr/bin:/bin HOME="${work}/home-that-does-not-exist" TMPDIR="${work}/tmp" \
    CLANKER_PARAMETER_PATH="$1" AWS_REGION="${region}" CLANKER_TEST_AWS_ENDPOINT="${aws}" \
    "${work}/clanker" up --cloud aws --env "${env_name}" --demo=false --fake-aws \
    --addr "127.0.0.1:${port}" --source "${work}/source" >"$2" 2>&1 &
  core=$!
  for _ in $(seq 1 30); do
    if curl -fs -o /dev/null "http://127.0.0.1:${port}/healthz"; then return 0; fi
    if ! kill -0 "${core}" 2>/dev/null; then break; fi
    sleep 1
  done
  echo "FAILED: the core did not start; see $2"
  tail -20 "$2"
  exit 1
}

stop_core() {
  kill "${core}"
  wait "${core}" 2>/dev/null || true
  core=""
}

# wait_state waits until the work item $1 is in the state $2, at most $3 seconds.
wait_state() {
  local state=""
  for _ in $(seq 1 $(($3 / 5))); do
    state="$(curl -fs -b "${work}/admin.jar" "${base}/api/v1/work-items/$1" | jq -r .work_item.state)"
    if [ "${state}" = "$2" ]; then return 0; fi
    case "${state}" in Failed | Rejected) break ;; esac
    sleep 5
  done
  echo "FAILED: $1 is in the state ${state}, not $2"
  sql "SELECT layer, kind, status, right(log, 1500) FROM jobs ORDER BY id DESC LIMIT 2"
  exit 1
}

echo "== build the core, the stand-ins and the runner image"
cd "${repo}"
if [ -n "$(git status --porcelain)" ]; then
  echo "the checkout has changes that are not committed; the runner image holds only committed files"
  exit 1
fi
go build -o "${work}/clanker" ./cmd/clanker
go build -o "${work}/fakeidp" ./scripts/verify/fakeidp
go build -o "${work}/fakeaws" ./scripts/verify/fakeaws
if [ -z "${image}" ]; then
  image="clanker-runner:aws-check-$$"
  own_image="${image}"
  make runner-image RUNNER_IMAGE="${image}" >"${work}/image.log" 2>&1 ||
    {
      tail -20 "${work}/image.log"
      exit 1
    }
fi

echo "== the source files, with the stand-in stacks as layers 10 to 50 of AWS"
mkdir "${work}/source" "${work}/cwd" "${work}/tmp" "${work}/tasks"
excludes=()
for stack in scripts/verify/fakeaws/fixture/*/; do
  excludes+=(":(exclude)platform/stacks/aws/$(basename "${stack}")")
done
git archive --format=tar HEAD platform runbooks apps/paperboat "${excludes[@]}" |
  tar -x -C "${work}/source"
for stack in scripts/verify/fakeaws/fixture/*/; do
  cp -R "${stack%/}" "${work}/source/platform/stacks/aws/"
done

echo "== MinIO with the bucket ${bucket}, and a database"
docker network create "${network}" >/dev/null
docker run -d --name "${minio}" --network "${network}" --network-alias minio \
  -p 127.0.0.1::9000 --env MINIO_ROOT_USER="${minio_key}" \
  --env MINIO_ROOT_PASSWORD="${minio_secret}" \
  --env MINIO_KMS_SECRET_KEY="aws-check:$(head -c 32 /dev/urandom | base64)" \
  "${minio_image}" >/dev/null
minio_address="$(docker port "${minio}" 9000/tcp | head -n 1)"
docker run -d --name "${database}" -p 127.0.0.1::5432 -e POSTGRES_PASSWORD=check \
  -e POSTGRES_DB=clanker postgres:18.7 >/dev/null
db_address="$(docker port "${database}" 5432/tcp | head -n 1)"
for attempt in $(seq 1 30); do
  if aws_cli s3api create-bucket --bucket "${bucket}" >/dev/null 2>&1; then break; fi
  if [ "${attempt}" = 30 ]; then
    echo "FAILED: MinIO did not take the bucket"
    exit 1
  fi
  sleep 1
done
for _ in $(seq 1 60); do
  if docker exec "${database}" pg_isready -U postgres -d clanker >/dev/null 2>&1; then break; fi
  sleep 1
done

echo "== the settings in the stand-in for Parameter Store"
role="arn:aws:iam::111122223333:role/clanker-check"
identities="$(jq -n --arg role "${role}" '{planner: ($role + "/planner")} +
  (["10-network", "20-cluster", "30-data", "40-platform", "50-app"] |
    map({key: ("applier-" + .), value: ($role + "/applier-" + .)}) | from_entries)')"
bootstrap="$(jq -n --argjson identities "${identities}" --arg bucket "${bucket}" \
  --arg region "${region}" '{
  clanker_ttl: {sensitive: false, type: "string", value: "168"},
  evidence_bucket: {sensitive: false, type: "string", value: "evidence"},
  identities: {sensitive: false, type: ["map", "string"], value: $identities},
  permission_guard: {sensitive: false, type: "string",
    value: "arn:aws:iam::111122223333:policy/clanker-check/guard"},
  state_backend: {sensitive: false, type: ["object", {}], value: {bucket: $bucket,
    encrypt: "true", region: $region, use_lockfile: "true"}},
  workload_role_path: {sensitive: false, type: "string", value: "/clanker-check/workload/"}
}')"
runner='{"cluster": "clanker-check", "task_definition": "clanker-runner:1",
  "container": "runner", "subnets": ["subnet-0a1b2c3d4e5f60718"],
  "security_groups": ["sg-0a1b2c3d4e5f60718"]}'
variables="$(jq -n --arg network "${network_id}" --arg team "${team_tag}" '{
  existing_network_id: $network,
  existing_private_subnet_ids: ["subnet-0a1b2c3d4e5f60718", "subnet-0f1e2d3c4b5a69788"],
  image_registry: "registry.example.com/check",
  instance_tags: {team: $team, purpose: "aws-core-check"},
  operator_principal_arn: "arn:aws:iam::111122223333:role/check-operator"}')"
jq -n --arg db "postgres://postgres:check@${db_address}/clanker?sslmode=disable" \
  --arg key "$("${repo}/scripts/verify/signing-key.sh")" --arg idp "${idp}" \
  --arg base "${base}" --arg core "http://host.docker.internal:${port}" \
  --arg bootstrap "${bootstrap}" --arg runner "${runner}" --arg variables "${variables}" \
  --arg slack "${aws}/hooks/slack" '{
  CLANKER_DATABASE_URL: $db, CLANKER_SIGNING_KEY: $key, CLANKER_OIDC_ISSUER: $idp,
  CLANKER_OIDC_CLIENT_ID: "clanker", CLANKER_OIDC_CLIENT_SECRET: "check",
  CLANKER_PUBLIC_URL: $base, CLANKER_ADMIN_EMAIL: "admin@example.com",
  CLANKER_APPROVER_EMAILS: "approver2@example.com", CLANKER_CORE_URL: $core, CLANKER_AWS_BOOTSTRAP: $bootstrap, CLANKER_AWS_RUNNER: $runner,
  CLANKER_AWS_LAYER_VARIABLES: $variables, CLANKER_SLACK_WEBHOOK: $slack} |
  {"/src/check": ., "/src/standby": del(.CLANKER_AWS_RUNNER)}' >"${work}/parameters.json"

echo "== start the stand-ins"
"${work}/fakeidp" --addr "127.0.0.1:${idp_port}" >"${work}/idp.log" 2>&1 &
provider=$!
"${work}/fakeaws" --addr "127.0.0.1:${aws_port}" --parameters "${work}/parameters.json" \
  --record "${work}/record.jsonl" --tokens "${work}/tokens.txt" --logs "${work}/tasks" \
  --image "${image}" --network "${network}" --minio "http://${minio_address}" \
  --minio-access-key "${minio_key}" --minio-secret-key "${minio_secret}" \
  >"${work}/fakeaws.log" 2>&1 &
standin=$!
for _ in $(seq 1 30); do
  if curl -fs -o /dev/null "${idp}/.well-known/openid-configuration" &&
    [ "$(status "${aws}/hooks/ready")" = 200 ]; then break; fi
  sleep 1
done
cd "${work}/cwd"

echo "== without the runner setting the core stands by and names the setting"
start_core /src/standby "${work}/standby.log"
sleep 3
stop_core
grep -q 'stands by.*reason="set CLANKER_AWS_RUNNER"' "${work}/standby.log" ||
  fail "the core without CLANKER_AWS_RUNNER did not stand by and name the setting"
[ "$(sql "SELECT count(*) FROM work_items")" = 0 ] ||
  fail "the core that stands by made a work item"

echo "== with every setting the core plans 10-network in a runner task"
start_core /src/check "${work}/up.log"
if [ "$(sign_in admin@example.com "${work}/admin.jar")" != admin ]; then
  echo "FAILED: the admin could not sign in; see ${work}/up.log and ${work}/idp.log"
  exit 1
fi
item="$(curl -fs -b "${work}/admin.jar" "${base}/api/v1/work-items" | jq -r '.[0].id')"
if [ -z "${item}" ] || [ "${item}" = null ]; then
  echo "FAILED: the core made no work item"
  exit 1
fi
wait_state "${item}" AwaitingApproval 600
detail="$(curl -fs -b "${work}/admin.jar" "${base}/api/v1/work-items/${item}" | jq .work_item)"
echo "${item}: $(jq -r '"\(.title), tier \(.tier) by \(.tier_rule), layers \(.layers | join(" "))"' <<<"${detail}")"
[ "$(jq -r '.layers | join(" ")' <<<"${detail}")" = "10-network 20-cluster 30-data 40-platform 50-app" ] ||
  fail "the first deployment does not have the layers 10 to 50"
[ "$(jq -r .tier <<<"${detail}")" = T3 ] || fail "the first deployment is not tier T3"
facts="$(sql "SELECT facts FROM plans WHERE work_item = '${item}' AND layer = '10-network'")"
[ "$(jq -c '[.add, .change, .destroy, .outputs]' <<<"${facts}")" = \
  '[0,0,0,["image_registry","network_id","private_subnet_ids","state_location"]]' ] ||
  fail "the plan of 10-network does not change only its outputs: ${facts}"

echo "== the calls to the stand-in for AWS"
record="${work}/record.jsonl"
[ "$(jq -s '[.[] | select(.service == "ssm")] | length' "${record}")" -ge 4 ] ||
  fail "the core did not read Parameter Store page by page at each start"
[ "$(jq -s '[.[] | select(.service != "webhook" and .access_key != "fake-aws")] | length' "${record}")" = 0 ] ||
  fail "a call to the stand-in was not signed with the keys of --fake-aws"
plan_session="$(jq -cs '[.[] | select(.action == "AssumeRole")][0].detail' "${record}")"
[ "$(jq -r '.role_arn + " " + .tags.clanker_layer + " " + .tags.clanker_env' <<<"${plan_session}")" = \
  "${role}/planner 10-network ${env_name}" ] ||
  fail "the first STS session is not a planner session for 10-network: ${plan_session}"
task="$(jq -cs '[.[] | select(.action == "RunTask")][0].detail' "${record}")"
[ "$(jq -r '[.cluster, .task_definition, .launch_type, .assign_public_ip, .container,
  .started_by, .core_url, .tags.clanker_env, (.environment_names | sort | join(","))] |
  join(" ")' <<<"${task}")" = "clanker-check clanker-runner:1 FARGATE DISABLED runner clanker \
http://host.docker.internal:${port} ${env_name} CLANKER_CORE_URL,CLANKER_JOB_TOKEN" ] ||
  fail "the runner task is not what CLANKER_AWS_RUNNER says: ${task}"
plan_log="$(sql "SELECT log FROM jobs WHERE work_item = '${item}' AND layer = '10-network' AND kind = 'plan'")"
grep -q -- "-backend-config=key=${env_name}/10-network.tfstate" <<<"${plan_log}" ||
  fail "the plan job did not use the state key of its layer"

echo "== the admin and a second approver approve; every layer is applied"
[ "$(sign_in approver2@example.com "${work}/approver.jar")" = approver ] ||
  fail "the second approver could not sign in"
for jar in admin approver; do
  [ "$(status -b "${work}/${jar}.jar" -H 'Content-Type: application/json' \
    -d '{"decision": "approve"}' "${base}/api/v1/work-items/${item}/decision")" = 200 ] ||
    fail "the ${jar} could not approve ${item}"
done
wait_state "${item}" Done 1200
echo "${item} is Done"

echo "== the state in the bucket"
keys="$(aws_cli s3api list-objects-v2 --bucket "${bucket}" --query 'Contents[].Key' --output text)"
want="${env_name}/10-network.tfstate	${env_name}/20-cluster.tfstate	${env_name}/30-data.tfstate	"
want+="${env_name}/40-platform.tfstate	${env_name}/50-app.tfstate"
[ "${keys}" = "${want}" ] || fail "the bucket must hold the state of each layer and no lock: ${keys}"
[ "$(aws_cli s3api head-object --bucket "${bucket}" --key "${env_name}/10-network.tfstate" \
  --query ServerSideEncryption --output text)" = AES256 ] ||
  fail "the state of 10-network is not encrypted"
network_state="$(aws_cli s3 cp "s3://${bucket}/${env_name}/10-network.tfstate" -)"
[ "$(jq -r '.outputs.network_id.value' <<<"${network_state}")" = "${network_id}" ] ||
  fail "the state of 10-network does not hold the network of the account"
[ "$(jq -c '.outputs.private_subnet_ids.value' <<<"${network_state}")" = \
  '["subnet-0a1b2c3d4e5f60718","subnet-0f1e2d3c4b5a69788"]' ] ||
  fail "the list of subnets did not reach 10-network as a list"
cluster_state="$(aws_cli s3 cp "s3://${bucket}/${env_name}/20-cluster.tfstate" -)"
[ "$(jq -c '[.outputs.network_id.value, .outputs.instance_tag_keys.value]' <<<"${cluster_state}")" = \
  "[\"${network_id}\",[\"purpose\",\"team\"]]" ] ||
  fail "20-cluster did not read 10-network from the bucket or did not get the map of tags"
[ "$(sql "SELECT outputs FROM layer_outputs WHERE env = '${env_name}' AND layer = '10-network'" |
  jq -r .network_id)" = "${network_id}" ] ||
  fail "the outputs of 10-network did not reach layer_outputs"

echo "== the sessions, the tasks and the notifications"
sessions="$(jq -s '[.[] | select(.action == "AssumeRole")] | length' "${record}")"
tasks="$(jq -s '[.[] | select(.action == "RunTask")] | length' "${record}")"
issued="$(sql "SELECT count(*) FROM events WHERE type = 'credential.issued'")"
echo "${sessions} STS sessions, ${tasks} runner tasks, ${issued} credential.issued events"
[ "${tasks}" = 10 ] || fail "want a plan task and an apply task for each of 5 layers"
if [ "${sessions}" != 10 ] || [ "${issued}" != 10 ]; then
  fail "want one STS session and one credential.issued event for each job"
fi
own_roles="$(jq -s --arg role "${role}" '[.[] | select(.action == "AssumeRole") | .detail |
  select(.role_arn == ($role + "/applier-" + .tags.clanker_layer))] | length' "${record}")"
[ "${own_roles}" = 5 ] || fail "want the apply of each layer in a session of its own applier role"
[ "$(sql "SELECT count(*) FROM work_items")" = 1 ] ||
  fail "the core made more than the first work item: $(sql "SELECT id, title FROM work_items")"
failed="$(sql "SELECT type || ': ' || summary FROM events WHERE type LIKE '%.failed'")"
[ -z "${failed}" ] || fail "failures in the audit log: ${failed}"
[ "$(sql "SELECT count(*) FROM deliveries WHERE channel = 'email' AND status <> 'skipped'")" = 0 ] ||
  fail "the core sent email with no CLANKER_SMTP_ADDR"
links="$(jq -rs '[.[] | select(.service == "webhook") | .detail.body] | join("\n")' "${record}")"
grep -q "${base}/work/${item}" <<<"${links}" ||
  fail "no Slack message has a link to ${base}/work/${item}"
if grep -q "127.0.0.1:${port}" <<<"${links}"; then fail "a Slack message links to the listen address"; fi

stop_core
echo "== the log of the core"
if grep -q "level=ERROR" "${work}/up.log" "${work}/standby.log"; then
  fail "error lines in the log of the core; see ${work}/up.log"
fi
while read -r token; do
  if grep -qF -- "${token}" "${work}/up.log"; then fail "a job token is in the log of the core"; fi
done <"${work}/tokens.txt"
for value in "${network_id}" "${team_tag}"; do
  if grep -qF -- "${value}" "${work}/up.log"; then fail "a value of the account is in the log"; fi
done
if [ -n "$(ls -A "${work}/cwd")" ]; then fail "files in the working directory of the core"; fi
if [ -n "$(ls -A "${work}/tmp")" ]; then fail "files in the temporary directory of the core"; fi
if [ -e "${work}/home-that-does-not-exist" ]; then fail "the core made its home directory"; fi

if [ "${failures}" -ne 0 ]; then
  echo "RESULT: ${failures} checks failed. The files are in ${work}"
  exit 1
fi
echo "RESULT: the core on AWS read its settings from Parameter Store, planned 10-network in" \
  "a runner task with an STS session and S3 state, and applied layers 10 to 50 after two" \
  "approvals"
