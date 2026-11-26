# AWS target

These six root modules implement `platform/CONTRACT.md` on AWS. Each directory is one OpenTofu
state. The state key is `<env_name>/<layer>.tfstate` in the bucket `state_location`.

Each root has the input `region`, which is not in the contract. The AWS provider and the
`terraform_remote_state` data sources need it. Some layers have more inputs that are not in
the contract. The next table lists them.

| Layer | Input | Meaning |
|---|---|---|
| `00-bootstrap` | `core_parameter_path` | Parameter Store path of the settings of the control plane. No agent role can read it. Default `/src` |
| `10-network` | `existing_network_id`, `existing_private_subnet_ids` | A VPC that exists and two or more of its private subnets, one in each zone. With them the layer makes no network and passes these IDs on. Empty: a new VPC |
| `10-network` | `image_registry` | Registry host and path prefix of the Paperboat images, for example the ECR registry that the installer makes. No default |
| `20-cluster` | `node_ami_type` | `AL2023_ARM_64_STANDARD` (default, Graviton) or `AL2023_x86_64_STANDARD`. `node_size` must have the same architecture. Its default is `t4g.medium` |
| `20-cluster` | `instance_tags` | Tags that the organization requires on each instance launch. The owner gives the values at run time. Do not write them into a file in this repository |
| `20-cluster` | `operator_principal_arn` | IAM role of the operator. It becomes a cluster admin, so the operator can remove the environment with their own credentials. Empty: no entry |

## What each layer creates

| Layer | Resources |
|---|---|
| `00-bootstrap` | S3 state bucket and S3 evidence bucket (versioned, encrypted, no public access, TLS only; with `ttl_hours` above 0 a destroy removes them with their objects). One permission boundary policy. IAM roles `observer`, `planner`, `debugger` and `applier-<layer>` for layers 10 to 50 |
| `10-network` | VPC, one public and one private subnet in each of `zone_count` zones, internet gateway, one NAT gateway, route tables. With `existing_network_id`: nothing. The layer reads each subnet and stops the plan if a subnet is not in the VPC |
| `20-cluster` | EKS control plane, one managed node group (Amazon Linux 2023, arm64 on `t4g.medium` by default) with a launch template that requires IMDSv2 and tags each instance and volume, the EKS Pod Identity agent add-on, the EFS CSI driver add-on with its pod identity role, IAM roles for the cluster and the nodes, EKS access entries for the agent roles and the operator |
| `30-data` | RDS for PostgreSQL (`db.t4g.micro`, one zone, gp3, encrypted, private, admin password in Secrets Manager), EFS file system with one mount target in each private subnet, `StorageClass` `efs-rwx`, S3 backup bucket (removed with its objects when `00-bootstrap` has a TTL) |
| `40-platform` | Calls `modules/shared/platform`. That module installs Envoy Gateway and the Gateway of the platform. The annotations from this root make the service controller of EKS create an internal Network Load Balancer for the Envoy `Service` (`clanker-envoy`), in the private subnets of `10-network`. The module reads the DNS name of the load balancer from that `Service` after the apply |
| `50-app` | Calls `modules/shared/app`, which installs the Paperboat chart |

### The load balancer

The service controller of the AWS cloud provider, which runs in the EKS control plane, makes
the load balancer. `40-platform` sets these `Service` annotations:

- `service.beta.kubernetes.io/aws-load-balancer-type: nlb`: a Network Load Balancer, not a
  Classic Load Balancer.
- `service.beta.kubernetes.io/aws-load-balancer-internal: "true"`: internal, not
  internet-facing.
- `service.beta.kubernetes.io/aws-load-balancer-subnets`: the private subnets of `10-network`.
  With explicit subnets the controller needs no `kubernetes.io/role/*` tags on the subnets.

The AWS Load Balancer Controller is not installed. Its `aws-load-balancer-scheme` annotation
has no effect on this controller.

### The tags of the node instances

The launch template of the node group puts `instance_tags` and the `clanker_*` tags on each
instance and each volume that the node group launches. A `clanker_*` tag wins over an entry
of `instance_tags` with the same key. Each value of `instance_tags` is marked sensitive, so
the control plane gives it to its scrubber when it reads the plan. The `clanker_*` values
stay readable.

## Identities

Only `broker_principal` is in the trust policy of each role. Each role has the permission
boundary and a maximum session of one hour.

| Role | Standing use | Condition to assume it |
|---|---|---|
| `observer` | Yes | None |
| `planner` | Yes | None |
| `debugger` | No | Session tags `clanker_grant` (not empty) and `clanker_env` |
| `applier-<layer>` | No | Session tags `clanker_grant` (not empty), `clanker_env` and `clanker_layer` = `<layer>` |

The broker passes the session tags in `sts:AssumeRole`. The broker's own IAM policy decides
when it can do so. The session tags are in CloudTrail for each call. The broker also passes
`clanker_work_item` and `clanker_plan`, the first 12 characters of the plan hash, which no
condition reads. The role session name is the work item and the grant ID.

The control plane reads the setting `CLANKER_AWS_BOOTSTRAP`: what `tofu output -json` prints
in `00-bootstrap`. It takes the role ARNs from the output `identities`, and the state backend
and the region of the other layers from the output `state_backend`. At each start it records
these outputs as the outputs of `00-bootstrap`. A setting with an output that the layer does
not have, with a missing output or with a sensitive output stops the start.

The setting `CLANKER_AWS_LAYER_VARIABLES` holds the inputs of layers `10-network` to
`50-app` that belong to the account, as JSON, for example
`{"existing_network_id": "vpc-...", "existing_private_subnet_ids": ["subnet-...", "subnet-..."], "image_registry": "...", "instance_tags": {"<key>": "<value>"}, "operator_principal_arn": "arn:aws:iam::...:role/..."}`.
Each job of those layers gets each one as `TF_VAR_<name>`. The scrubber of the control plane
learns each string value at the start, before any plan reaches a model. It ignores a value
under 8 bytes, and the start log names the variables with such a value.

The control plane plans the first deployment when it has `CLANKER_AWS_BOOTSTRAP` (the state
backend and the roles of the broker) and `CLANKER_AWS_RUNNER` (the runner tasks). Without one
of them it serves the dashboard, proposes no deployment and logs the missing setting.

The permission boundary allows only the services that the platform uses. It denies IAM users
and access keys, roles without the boundary, changes to the boundary and to the agent roles,
changes to the configuration of the state and evidence buckets, and deletion of evidence.

The boundary also denies these reads to every agent role, so that a plan of code from a pull
request cannot get a secret of the control plane:

- `ssm:GetParameter*` on the settings of the control plane. They hold its signing key and its
  database URL. The input `core_parameter_path` of `00-bootstrap` names the path. The default
  `/src` covers each path that the installer uses (`/src/<name>`).
- `ssm:GetParametersByPath` on every path. Parameter Store checks this call against the
  requested path only, so a recursive read of `/` would return the settings anyway.
- `ecs:DescribeTasks` and `ecs:ListTasks`. The job token of a runner is an environment
  override of `ecs:RunTask`, and `DescribeTasks` shows it.

## Bootstrap sequence

1. The `terraform.tfvars` files do not set `env_name`, `state_location`, `region` or
   `broker_principal`, because a value in the file replaces a `TF_VAR_*` value. The job
   gives them: `state_location` is a bucket name that is free for `00-bootstrap` and the
   same bucket for the other five layers, and `broker_principal` is the ARN of the broker's
   IAM role. With an S3 backend in the job, the runner sets `state_location` to the bucket
   of the backend. A plan without these values fails with a missing variable.
2. `00-bootstrap` creates the state bucket, so its first apply cannot use that bucket.
   `clanker bootstrap` applies the layer with local state and then moves the state to S3.
3. Give `image_registry` to `10-network`, and the existing VPC and subnets if the
   environment uses them. Give `instance_tags` and `operator_principal_arn` to `20-cluster`.
   The control plane gives them from `CLANKER_AWS_LAYER_VARIABLES`.
4. Apply the layers in sequence from `10-network` to `50-app`. Each layer reads the layers
   below it from the state bucket.
5. Push the Paperboat images to `<image_registry>/paperboat` before `50-app`.

The machine that runs `tofu` for layers 30 to 50 needs the AWS CLI, because the `kubernetes`
and `helm` providers get a token from `aws eks get-token`.

## Cost estimate

This is an estimate. It uses on-demand list prices for `us-east-1` and the values in the
`terraform.tfvars` files. It does not include tax or data transfer to the internet. Check the
AWS pricing pages before you rely on it.

| Item | Size | USD each hour | USD each month (730 hours) |
|---|---|---|---|
| EKS control plane, standard support | 1 cluster | 0.100 | 73 |
| Worker nodes | 2 × `t4g.medium` | 0.067 | 49 |
| Node disks | 2 × 20 GiB gp3 | 0.004 | 3 |
| NAT gateway | 1, plus about 20 GB processed | 0.046 | 34 |
| Network Load Balancer | 1, internal, low traffic | 0.024 | 18 |
| Public IPv4 addresses | 1 (NAT gateway) | 0.005 | 4 |
| RDS for PostgreSQL | `db.t4g.micro`, one zone | 0.016 | 12 |
| RDS storage | 20 GiB gp3 | 0.003 | 2 |
| EFS | about 1 GiB, elastic throughput | 0.001 | 1 |
| S3, Secrets Manager | 3 small buckets, 1 secret | 0.002 | 2 |
| **Total** | | **about 0.27** | **about 200** |

With `existing_network_id`, `10-network` makes no NAT gateway and no public IPv4 address, so
the total is about 0.22 USD each hour (about 160 each month). The images are in the registry
of the installer, which is not in these layers.

The fixed charges are about 80 % of the total. The cluster, the NAT gateway and the load
balancer cost the same with no traffic.

## What is validated and what is not

Validated, with no errors and no warnings:

- `tofu fmt -check -recursive platform`
- `tofu init -backend=false` and `tofu validate` in each of the six root modules
- `tofu test` with mock providers in each root module that has a `tests` directory. The tests
  check the configuration that OpenTofu plans, not the behaviour of AWS

Not done:

- No `tofu plan` and no `tofu apply`. No AWS account was used. IAM policy content, EKS add-on
  defaults, version availability and service quotas are not tested.
- The conformance suite did not run against AWS.
- `40-platform` and `50-app` were validated against stub copies of `modules/shared/platform`
  and `modules/shared/app` that contain only variables and outputs.

## Known limits

- The applier roles have service-level permissions inside the boundary. Make them narrower
  after the first real plan shows the exact actions.
- `applier-30-data`, `applier-40-platform` and `applier-50-app` are cluster administrators in
  Kubernetes. `debugger` can edit only the `paperboat` namespace.
- With `existing_network_id`, `10-network` checks that each subnet is in the VPC. It does not
  check that the subnets are private or that each is in a different zone. EFS, RDS and the
  load balancer need one subnet in each zone.
- AWS gives the service controller of the cloud provider only critical bug fixes, and
  recommends the AWS Load Balancer Controller for new load balancers.
- The RDS admin secret rotates. After a rotation, apply `40-platform` again to refresh
  `DATABASE_URL`.
- The Kubernetes API endpoint is public and private. IAM protects the public endpoint.
- No pod has an IAM role for the backup bucket yet. The component that writes backups needs
  an EKS Pod Identity association.
- CloudTrail is the audit source in the plan. These modules do not create a trail.
