# shellcheck shell=bash
# shellcheck disable=SC2154 # call, state, arg and refuse come from the aws stand-in.
# The answers of the aws stand-in to the calls of the installer and of the removal. The
# stand-in sources this file with "call", "state", "arg" and "refuse" set. What the calls
# make is kept in the state directory: the images that ops made, the images that docker
# pushed, the parameters, and markers that the tofu stand-in writes for the home stack.

account="111122223333"
images="${state}/images"
snapshots="${state}/snapshots"
touch "${images}" "${snapshots}" "${state}/taskdefs"

not_found() {
  refuse "An error occurred ($1) when calling the $2 operation: $3"
}

# list prints a JSON list of its arguments.
list() {
  local first=true item
  printf '['
  for item in "$@"; do
    if "${first}"; then first=false; else printf ','; fi
    printf '"%s"' "${item}"
  done
  printf ']\n'
}

# lines prints a JSON list of the lines of its input.
lines() {
  awk 'BEGIN { printf "["; n = 0 } { if (n++) printf ","; printf "\"%s\"", $0 } END { print "]" }'
}

# left prints a list that is empty when the home stack holds no resource.
left() {
  if applied; then list "$1"; else list; fi
}

describe_images() {
  local filters
  filters="$*"
  case "${filters}" in
  *"Name=image-id,Values="*)
    local id
    id="${filters##*Name=image-id,Values=}"
    id="${id%% *}"
    awk -v id="${id}" 'BEGIN { printf "["; n = 0 }
      $1 == id { if (n++) printf ","; printf "{\"name\":\"%s\"}", $3 }
      END { print "]" }' "${images}"
    ;;
  *"Name=tag:Name,Values="*)
    local name
    name="${filters##*Name=tag:Name,Values=}"
    name="${name%% *}"
    awk -v name="${name}" 'BEGIN{printf "["; n=0}
      $3 == name {if (n++) printf ","; printf "{\"id\":\"%s\",\"snapshot\":\"%s\"}", $1, $2}
      END{print "]"}' "${images}"
    ;;
  *"Images[].ImageId"*)
    awk '$4 == "tagged" { print $1 }' "${images}" | lines
    ;;
  *"Name=tag:clanker_env"*)
    awk 'BEGIN{printf "["; n=0}
      $4 == "tagged" {if (n++) printf ","
        printf "{\"id\":\"%s\",\"snapshot_id\":\"%s\",\"name\":\"%s\"}", $1, $2, $3}
      END{print "]"}' "${images}"
    ;;
  *) refuse "the fixture has no answer for: aws ec2 describe-images ${filters}" ;;
  esac
}

ecr_images() {
  local name image dir
  name="$(arg --repository-name "$@")"
  image="$(arg --image-ids "$@")"
  dir="${state}/ecr/$(echo "${name}" | tr / _)"
  case "${image}" in
  imageTag=*)
    if [ -f "${dir}/${image#imageTag=}" ]; then
      printf '"%s"\n' "$(cat "${dir}/${image#imageTag=}")"
      return
    fi
    ;;
  imageDigest=*)
    if [ -d "${dir}" ] && grep -rqx "${image#imageDigest=}" "${dir}"; then
      printf '"%s"\n' "${image#imageDigest=}"
      return
    fi
    ;;
  esac
  not_found ImageNotFoundException DescribeImages "The image with imageId ${image} does not exist"
}

case "${call}" in
"ec2 describe-security-groups") echo '["vpc-0f1c7e01"]' ;;
"route53 list-hosted-zones")
  echo '[{"id":"/hostedzone/Z0F1C7E1","name":"example.com."},{"id":"/hostedzone/Z0F1C7E2","name":"example.com."}]'
  ;;
"route53 get-hosted-zone")
  zone="$(arg --id "$@")"
  case "${zone}:$*" in
  Z0F1C7E1:*DelegationSet*)
    # The delegated zone has the name servers that the internet knows for example.com, so
    # that the check of the delegation passes with the real resolver of the program. A test
    # that replaces the resolver gives them in SRC_FIXTURE_NAME_SERVERS.
    servers="${SRC_FIXTURE_NAME_SERVERS:-$(dig +short NS example.com)}"
    if [ -z "${servers}" ]; then
      refuse "the stand-in found no name server of example.com: it needs DNS"
    fi
    echo "${servers}" | lines
    ;;
  Z0F1C7E2:*DelegationSet*) echo '["ns-1.awsdns-01.org","ns-2.awsdns-02.net"]' ;;
  Z0F1C7E[12]:*) echo '{"name":"example.com.","private":false}' ;;
  *) not_found NoSuchHostedZone GetHostedZone "No hosted zone found with ID: ${zone}" ;;
  esac
  ;;
"ssm get-parameter") echo '"ami-0f1c7e0000000aa1"' ;;
"ec2 run-instances")
  case "$*" in
  *"example:owner"*)
    refuse "An error occurred (DryRunOperation) when calling the RunInstances operation: Request would have succeeded, but DryRun flag is set."
    ;;
  *)
    refuse "An error occurred (UnauthorizedOperation) when calling the RunInstances operation: You are not authorized to perform this operation. User: arn:aws:sts::${account}:assumed-role/Operator/fixture is not authorized to perform: ec2:RunInstances with an explicit deny in a service control policy. Encoded authorization failure message: ENCODED-FIXTURE-MESSAGE-$(printf '%0900d' 0)"
    ;;
  esac
  ;;
"sts decode-authorization-message")
  echo '"{\"allowed\":false,\"explicitDeny\":true,\"matchedStatements\":{\"items\":[{\"effect\":\"DENY\",\"conditions\":{\"items\":[{\"key\":\"aws:RequestTag/example:owner\",\"values\":{\"items\":[{\"value\":\"true\"}]}}]}}]}}"'
  ;;
"ecr get-login-password") echo "fixture-registry-token" ;;
"ecr describe-images") ecr_images "$@" ;;
"ecr list-images")
  dir="${state}/ecr/$(arg --repository-name "$@" | tr / _)"
  if [ -d "${dir}" ]; then find "${dir}" -type f | wc -l | tr -d ' '; else echo 0; fi
  ;;
"ec2 describe-images") describe_images "$@" ;;
"ec2 create-tags")
  for resource in $(arg --resources "$@") $(echo "$*" | sed -n 's/.*--resources [^ ]* \(snap-[^ ]*\).*/\1/p'); do
    sed -i.bak "s/^\(${resource} .*\) untagged$/\1 tagged/" "${images}" "${snapshots}"
  done
  echo '{}'
  ;;
"ec2 deregister-image")
  id="$(arg --image-id "$@")"
  grep -v "^${id} " "${images}" >"${images}.new" || true
  mv "${images}.new" "${images}"
  echo '{}'
  ;;
"ec2 describe-snapshots")
  case "$*" in
  *"Name=snapshot-id,Values="*)
    id="$*"
    id="${id##*Name=snapshot-id,Values=}"
    id="${id%% *}"
    awk -v id="${id}" '$1 == id { print $1 }' "${snapshots}" | lines
    ;;
  *) awk '$3 == "tagged" { print $1 }' "${snapshots}" | lines ;;
  esac
  ;;
"ec2 delete-snapshot")
  id="$(arg --snapshot-id "$@")"
  grep -v "^${id} " "${snapshots}" >"${snapshots}.new" || true
  mv "${snapshots}.new" "${snapshots}"
  echo '{}'
  ;;
"elbv2 describe-target-health")
  if [ -f "${state}/phase2" ]; then echo '["healthy"]'; else echo '[]'; fi
  ;;
"ssm put-parameter")
  mkdir -p "${state}/parameters"
  value="$(arg --value "$@")"
  cp "${value#file://}" "${state}/parameters/$(arg --name "$@" | tr / _)"
  echo '{"Version":1}'
  ;;
"s3api head-object")
  if [ -f "${state}/s3/$(arg --key "$@" | tr / _)" ]; then echo '{}'; else
    not_found 404 HeadObject "Not Found"
  fi
  ;;
"s3api head-bucket")
  if [ -f "${state}/buckets" ]; then echo '{}'; else not_found 404 HeadBucket "Not Found"; fi
  ;;
"autoscaling describe-auto-scaling-groups")
  if [ ! -f "${state}/phase2" ]; then echo '[]'; elif [ -f "${state}/core-stopped" ]; then
    echo '[0]'
  else echo '[1]'; fi
  ;;
"autoscaling update-auto-scaling-group")
  touch "${state}/core-stopped"
  echo '{}'
  ;;
"ecs describe-clusters")
  if applied; then list "arn:aws:ecs:us-west-2:${account}:cluster/$(arg --clusters "$@")"; else list; fi
  ;;
"ecs list-tasks")
  if applied && [ ! -f "${state}/tasks-stopped" ]; then
    list "arn:aws:ecs:us-west-2:${account}:task/$(arg --cluster "$@")/0f1c7e"
  else list; fi
  ;;
"ecs stop-task")
  touch "${state}/tasks-stopped"
  echo '{}'
  ;;
"ecs describe-tasks") list ;;
"ecs list-task-definitions")
  prefix="$(arg --family-prefix "$@")"
  status="$(arg --status "$@")"
  awk -v prefix="${prefix}" -v status="${status:-ACTIVE}" \
    'index($1, "task-definition/" prefix) > 0 && $2 == status { print $1 }' \
    "${state}/taskdefs" | lines
  ;;
"ecs delete-task-definitions")
  for arn in $(echo "$*" | sed 's/.*--task-definitions //; s/ --.*//'); do
    grep -v "^${arn} " "${state}/taskdefs" >"${state}/taskdefs.new" || true
    mv "${state}/taskdefs.new" "${state}/taskdefs"
  done
  echo '{}'
  ;;
"iam list-roles") left "clanker-fixture-core" ;;
"iam list-policies") left "clanker-fixture-boundary" ;;
"iam list-instance-profiles") left "clanker-fixture-core" ;;
"logs describe-log-groups") left "/clanker/fixture/runner" ;;
"ssm get-parameters-by-path") left "/src/fixture/CLANKER_DATABASE_URL" ;;
"ecr describe-repositories") left "clanker-fixture/runner" ;;
"kms list-aliases") left "alias/clanker-fixture-settings" ;;
"route53 list-resource-record-sets") left "src-fixture.example.com." ;;
"resourcegroupstaggingapi get-resources") left "arn:aws:rds:us-west-2:${account}:db:clanker" ;;
*)
  echo "the fixture has no answer for: aws $*" >&2
  exit 1
  ;;
esac
