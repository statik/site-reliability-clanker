# AWS target

These six root modules implement `platform/CONTRACT.md` on AWS. Each directory is one OpenTofu
state. The state key is `<env_name>/<layer>.tfstate` in the bucket `state_location`.

Each root has one input that is not in the contract: `region`. The AWS provider and the
`terraform_remote_state` data sources need it.

## What each layer creates

| Layer | Resources |
|---|---|
| `00-bootstrap` | S3 state bucket and S3 evidence bucket (versioned, encrypted, no public access, TLS only). One permission boundary policy. IAM roles `observer`, `planner`, `debugger` and `applier-<layer>` for layers 10 to 50 |
| `10-network` | VPC, one public and one private subnet in each of `zone_count` zones, internet gateway, one NAT gateway, route tables. ECR repository `paperboat` |
| `20-cluster` | EKS control plane, one managed node group (Amazon Linux 2023, x86_64), the EKS Pod Identity agent add-on, the EFS CSI driver add-on with its pod identity role, IAM roles for the cluster and the nodes, EKS access entries for the agent roles |
| `30-data` | RDS for PostgreSQL (`db.t4g.micro`, one zone, gp3, encrypted, private, admin password in Secrets Manager), EFS file system with one mount target in each private subnet, `StorageClass` `efs-rwx`, S3 backup bucket |
| `40-platform` | Calls `modules/shared/platform`. That module installs Envoy Gateway. The annotations from this root make AWS create a Network Load Balancer for the Envoy `Service` |
| `50-app` | Calls `modules/shared/app`, which installs the Paperboat chart |

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

The control plane reads the role ARNs from the setting `CLANKER_AWS_IDENTITIES`: the output
`identities` of `00-bootstrap` as JSON.

The permission boundary allows only the services that the platform uses. It denies IAM users
and access keys, roles without the boundary, changes to the boundary and to the agent roles,
changes to the configuration of the state and evidence buckets, and deletion of evidence.

## Bootstrap sequence

1. In `00-bootstrap/terraform.tfvars`, set `state_location` to a bucket name that is free and
   set `broker_principal` to the ARN of the broker's IAM role. The values in the file are
   placeholders. Use the same `state_location` in the other five files.
2. `00-bootstrap` creates the state bucket, so its first apply cannot use that bucket.
   `clanker bootstrap` applies the layer with local state and then moves the state to S3.
3. Apply the layers in sequence from `10-network` to `50-app`. Each layer reads the layers
   below it from the state bucket.
4. Push the Paperboat images to `<image_registry>/paperboat` before `50-app`.

The machine that runs `tofu` for layers 30 to 50 needs the AWS CLI, because the `kubernetes`
and `helm` providers get a token from `aws eks get-token`.

## Cost estimate

This is an estimate. It uses on-demand list prices for `us-east-1` and the values in the
`terraform.tfvars` files. It does not include tax or data transfer to the internet. Check the
AWS pricing pages before you rely on it.

| Item | Size | USD each hour | USD each month (730 hours) |
|---|---|---|---|
| EKS control plane, standard support | 1 cluster | 0.100 | 73 |
| Worker nodes | 2 × `t3.medium` | 0.083 | 61 |
| Node disks | 2 × 20 GiB gp3 | 0.004 | 3 |
| NAT gateway | 1, plus about 20 GB processed | 0.046 | 34 |
| Network Load Balancer | 1, low traffic | 0.024 | 18 |
| Public IPv4 addresses | 3 (NAT gateway, load balancer in 2 zones) | 0.015 | 11 |
| RDS for PostgreSQL | `db.t4g.micro`, one zone | 0.016 | 12 |
| RDS storage | 20 GiB gp3 | 0.003 | 2 |
| EFS | about 1 GiB, elastic throughput | 0.001 | 1 |
| S3, ECR, Secrets Manager | 3 small buckets, 4 images, 1 secret | 0.002 | 2 |
| **Total** | | **about 0.30** | **about 215** |

The fixed charges are about 80 % of the total. The cluster, the NAT gateway and the load
balancer cost the same with no traffic.

## What is validated and what is not

Validated, with no errors and no warnings:

- `tofu fmt -check -recursive platform`
- `tofu init -backend=false` and `tofu validate` in each of the six root modules

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
- EC2 instances of the node group do not get the `clanker_*` tags. A managed node group
  without a launch template does not copy tags to its instances.
- The RDS admin secret rotates. After a rotation, apply `40-platform` again to refresh
  `DATABASE_URL`.
- The Kubernetes API endpoint is public and private. IAM protects the public endpoint.
- No pod has an IAM role for the backup bucket yet. The component that writes backups needs
  an EKS Pod Identity association.
- CloudTrail is the audit source in the plan. These modules do not create a trail.
- The ECR repository name `paperboat` is fixed, so two environments in one account and region
  cannot both create it.
