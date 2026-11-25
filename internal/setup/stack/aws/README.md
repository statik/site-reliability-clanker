# Home stack on AWS

This OpenTofu root module creates the home of the control plane on AWS: everything that must
exist before layer `10-network`. It is not a layer of `platform/CONTRACT.md`, and it is not in
`platform/`, so the forge of the control plane does not get it. The binary carries it
(`internal/setup/stack`). The installer applies it from a copy.

It has one local state. The owner keeps it: it holds the database password, the client
secret, the passwords of the two Keycloak users and the signing key of the broker. Put it at
a path that only the owner can read:

```
tofu init -backend-config=path=<directory with mode 0700>/home.tfstate
```

## What it creates

The stack uses an existing VPC and changes nothing in it.

| Part | Resources |
|---|---|
| Load balancer | Internal Application Load Balancer in the private subnets. ACM certificate for `src-<env>.<zone>` and `src-<env>-idp.<zone>`, validated with DNS records in the zone. Alias records for the two names. HTTPS listener (TLS 1.3 and 1.2) with one host rule for each name, and an HTTP listener that redirects to HTTPS |
| Core (phase 2) | Launch template (the Nanos image, `t4g.small`, IMDSv2 required, no key pair, the instance profile, the required tags on the instance and its volume) and an auto scaling group of one. Its health check is the health check of the load balancer on `/healthz`, port 7070 |
| Database | RDS for PostgreSQL 18 (the current minor version of RDS), `db.t4g.micro`, 20 GiB gp3, encrypted, private, one zone, one day of automatic backups, no deletion protection, no final snapshot. The URL has `sslmode=require` |
| Settings | A customer KMS key (deletion window of 7 days) and SecureString parameters below `/src/<env>`. Keycloak's own parameters are below `/src/<env>/idp` |
| Roles | The role of the core (the broker principal of `00-bootstrap`) with its instance profile. The execution roles of the runner and of Keycloak. No task role |
| Registry | ECR repository `clanker-<env>/runner` for the runner image |
| Runners | ECS cluster, log group (7 days) and, in phase 2, the Fargate task definition (ARM64, 1 vCPU, 2 GiB) with the runner image by its digest |
| Keycloak | Log group (7 days), Fargate task definition (ARM64, 1 vCPU, 2 GiB) and a service of one task behind the load balancer. Keycloak 26.8.2, pinned by digest, in production mode with `--db=dev-file` |
| Security groups | Load balancer: 443 and 80 from the router and the VPC. Core: 7070 from the load balancer and the runners. Keycloak: 8080 from the load balancer. Database: 5432 from the core. The core, the runners and Keycloak send to any address through the NAT gateway |

Every resource gets the tags of `required_tags` and `clanker_env = <env>`. The launch template
puts the same tags on the request of `ec2:RunInstances`, because some organizations deny a
launch without them.

### The settings of the core

The core reads the parameters directly below `/src/<env>` (`CLANKER_PARAMETER_PATH`) when it
starts. The image of the core holds only the path and the region (output `core_image_env`).

| Parameter | Value |
|---|---|
| `CLANKER_DATABASE_URL` | `postgres://clanker:<password>@<RDS host>:5432/clanker?sslmode=require` |
| `CLANKER_SIGNING_KEY` | An Ed25519 private key of the broker in base64: the seed, then the public key, as Go keeps it. 64 random bytes are not a key: the core could not check its own grants |
| `CLANKER_PUBLIC_URL` | `https://src-<env>.<zone>` |
| `CLANKER_OIDC_ISSUER` | `https://src-<env>-idp.<zone>/realms/<realm>` |
| `CLANKER_OIDC_CLIENT_ID` | The first client of the realm |
| `CLANKER_OIDC_CLIENT_SECRET` | Random, also in `/src/<env>/idp` for Keycloak |
| `CLANKER_ADMIN_EMAIL` | `admin@src-<env>-idp.<zone>` |
| `CLANKER_APPROVER_EMAILS` | `approver@src-<env>-idp.<zone>`: the core makes this user an approver when it starts, so a T3 change has its second approver |
| `CLANKER_AWS_RUNNER` | Phase 2: cluster, task definition, container, private subnets and the runner security group |
| `CLANKER_AWS_BOOTSTRAP` | A placeholder for the outputs of `00-bootstrap` (`tofu output -json`). Nothing writes them yet: `clanker bootstrap --cloud aws` refuses today. The writer must overwrite the parameter as a SecureString with `--key-id <parameter_key_arn>`, as the role that applies this stack. The stack ignores later changes of the value. With the placeholder the core does not start |

No value holds the address of an instance: people, Keycloak (the redirect URI) and the
collector (output `collector_endpoint`) reach the core by its name. The runners get the
private address of the core from the core, for each task.

### Who can read the settings

- The key policy lets the account manage the key, but not use it, so no IAM policy can
  grant its use. `applier-30-data` has `kms:Decrypt` on every key.
- Only three principals can decrypt, and only through Parameter Store: the role that applies
  this stack (the provider reads each parameter back), the role of the core (parameters below
  `/src/<env>`), and the execution role of Keycloak (parameters below `/src/<env>/idp`).
- The permission boundary of the agent roles denies `ssm:GetParameter*` below `/src`.

### Keycloak

Keycloak imports the realm of the input `keycloak_realm`: the content of
`scripts/verify/keycloak/realm.json`. A released binary does not carry `scripts/verify`, so
the realm comes from a checkout until the installer carries it.

Keycloak keeps a `${NAME}` whose variable is not set as literal text. Two checks stop that:

- The plan fails when the realm reads a variable that the container does not get.
- The entry point `keycloak-start.sh` stops the container when such a variable is empty. The
  service has the deployment circuit breaker, so the apply then fails.

The users are `admin@src-<env>-idp.<zone>` and `approver@src-<env>-idp.<zone>`, the second
approver of a T3 change. Their passwords are the outputs `admin_password` and
`approver_password`. Keycloak keeps its data in a file of the task: a new task imports the
realm again, and sessions end.

## Inputs

The owner gives the values of one account in `terraform.tfvars` (git ignores it; see
`terraform.tfvars.example`) and the realm in `TF_VAR_keycloak_realm`.

| Input | Meaning |
|---|---|
| `env_name` | The name of the environment, the same as in `00-bootstrap` |
| `region` | The region |
| `vpc_id`, `private_subnet_ids` | The VPC and two private subnets or more, in different zones, with a route to a NAT gateway |
| `router_security_group_id` | The security group of the Tailscale subnet router |
| `zone_id` | A public Route 53 zone of the same account |
| `required_tags` | The tags that the organization requires. `{}` for none |
| `keycloak_realm` | The realm, as JSON text |
| `core_instance_type` | Default `t4g.small`. It must match the architecture of the image |
| `runner_image_digest` | Phase 2 |
| `core_image_id` | Phase 2. It needs `runner_image_digest` |

## Order

1. **Phase 1.** Apply without `runner_image_digest` and `core_image_id`. This creates
   everything except the runner task definition and the core. The apply waits until Keycloak
   answers the discovery document of the realm behind the load balancer.
2. Push the runner image (`make runner-image`) to `runner_repository_url`, and note its
   digest.
3. Build the image of the core with `ops`, with `core_image_env` as its `Env`.
4. Apply `00-bootstrap` with `broker_principal = <core_role_arn>`, and write its outputs to
   `bootstrap_parameter` (see the table above). `clanker bootstrap` is to do both; it does
   not do them on AWS yet.
5. **Phase 2.** Apply with `runner_image_digest` and `core_image_id`. The apply waits until
   the core answers `/healthz` behind the load balancer (up to 15 minutes).

The core reads its settings and the discovery document of Keycloak only when it starts, and
stops when one of them is missing. Phase 2 therefore comes after step 4, and the auto
scaling group depends on the Keycloak service, the DNS names, the listener rules and every
parameter. A new runner digest changes `CLANKER_AWS_RUNNER`: the core reads it at its next
start. A new image of the core replaces the instance.

## Rights of the role that applies the stack

The role needs the services of the stack (EC2, Elastic Load Balancing, ACM, Route 53, RDS,
KMS, Systems Manager, ECS, ECR, CloudWatch Logs, Auto Scaling) and these IAM actions:
`iam:CreateRole`, `iam:GetRole`, `iam:TagRole`, `iam:DeleteRole`, `iam:PutRolePolicy`,
`iam:GetRolePolicy`, `iam:DeleteRolePolicy`, `iam:ListRolePolicies`,
`iam:ListAttachedRolePolicies`, `iam:ListInstanceProfilesForRole`,
`iam:CreateInstanceProfile`, `iam:GetInstanceProfile`, `iam:TagInstanceProfile`,
`iam:AddRoleToInstanceProfile`, `iam:RemoveRoleFromInstanceProfile`,
`iam:DeleteInstanceProfile` and `iam:PassRole` for the three roles. The data source
`aws_iam_session_context` calls `iam:GetRole` for the role of the session.

## Cost for one week

On-demand list prices, 168 hours, no tax. The load balancer and Fargate prices are from the
examples of their price pages, which use US East (N. Virginia). The pages did not show
US West (Oregon) prices, so those two rows are not confirmed for Oregon.

| Item | Size | USD each hour | USD for one week | Source |
|---|---|---|---|---|
| Core | `t4g.small` | 0.0168 | 2.82 | `internal/setup/plan.go`, Oregon, read 2026-11-18 |
| Database | `db.t4g.micro`, one zone | 0.016 | 2.69 | Same |
| Database storage | 20 GiB gp3 | | about 0.53 | 0.115 USD per GiB-month, not confirmed |
| Load balancer | ALB hours | 0.0225 | 3.78 | ELB price page, N. Virginia |
| Load balancer capacity | Low traffic, about 0.1 LCU | 0.0008 | about 0.13 | 0.008 USD per LCU-hour, N. Virginia |
| Keycloak | Fargate ARM64, 1 vCPU, 2 GiB | 0.0395 | 6.64 | 0.03238 per vCPU-hour, 0.00356 per GB-hour, N. Virginia |
| Runner tasks | 20 jobs of 10 minutes | | about 0.13 | Same Fargate prices |
| KMS key | 1 key | | 0.23 | 1 USD a month, hourly; no charge while it waits for deletion |
| NAT gateway processing | About 10 GB of image downloads | | about 0.45 | 0.045 USD per GB, not confirmed |
| Logs, ECR storage, standard parameters, ACM certificate, alias queries | Small | | under 0.20 | Not confirmed |
| **Total** | | | **about 17.60** | |

Keycloak is the largest item. The load balancer, Keycloak and the database cost the same
with no traffic.

## Teardown

1. Remove the layers above `00-bootstrap` (`clanker destroy`), if they exist.
2. Stop the runner tasks that still run in the cluster. A cluster with a running task
   cannot be deleted.
3. Destroy `00-bootstrap`. Its agent roles trust the role of the core, so they go first.
4. `tofu destroy` here, with the same inputs. It terminates the instance, stops Keycloak,
   deletes the database without a final snapshot and with its automatic backups, deletes the
   repository with its images, the parameters (with `CLANKER_AWS_BOOTSTRAP`), the log groups,
   the certificate, its validation records and the alias records, and schedules the
   deletion of the key (7 days).
5. Delete what is outside the state: the image of the core and its snapshot (`ops` made
   them), and the task definitions of the two families. `tofu destroy` only deregisters a
   task definition, which stays `INACTIVE`:
   `aws ecs delete-task-definitions --task-definitions <ARN of each revision>`.
6. Prove that nothing is left: the tagging API with the tag `clanker_env = <env>`, and lists
   of the IAM roles `clanker-<env>-*`, the task definitions `clanker-<env>-*`, the log groups
   `/clanker/<env>/`, the parameters below `/src/<env>`, the alias `alias/clanker-<env>-settings`
   (its key waits for deletion), and the records `src-<env>*` in the zone.

## What is validated and what is not

Validated:

- `tofu fmt -check`, `tofu init -backend=false`, `tofu validate`.
- `tofu test` with mock providers (`tests/home.tftest.hcl`): phase 1 and phase 2, the internal
  load balancer with TLS, no IP address in any output, parameter or Keycloak variable, the
  principals of the key policy, the statements of the role of the core, no task role, the
  value of `CLANKER_AWS_RUNNER`, the tags of the launch template, the signing key of a known
  Ed25519 key, the database, the settings that let a destroy remove everything, and the plan
  failure for a realm variable that the container does not get.
- The conversion of the signing key with 300 keys of the real `tls` provider, each checked
  with Go `crypto/ed25519`.
- `keycloak-start.sh` in the pinned Keycloak image with Docker: it stops on a missing or
  empty variable, and imports the realm with the values when all are set.
- The same image with the environment of the task definition (an `https://` name and
  `KC_PROXY_HEADERS=xforwarded`): a health check over plain HTTP without forwarded headers
  gets the discovery document with the `https://` issuer, and a request with
  `X-Forwarded-Proto: https` gets the login form from a tailnet address and from a public
  address.

Not done: no `tofu plan` and no `tofu apply` against AWS. Key policy, IAM conditions, the
availability of PostgreSQL 18 on `db.t4g.micro` in the region, service quotas and the tag
rules of the organization are not tested.
