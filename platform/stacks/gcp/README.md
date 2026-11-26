# Google Cloud target

These six root modules implement `platform/CONTRACT.md` on Google Cloud. Each root has one
OpenTofu state in the `gcs` backend, with the prefix `<env_name>/<layer>`. The building
blocks are in `platform/modules/gcp/`.

Each root takes `project_id` and `region` in addition to the contract inputs.

## What each layer creates

| Layer | Resources |
|---|---|
| `00-bootstrap` | The service APIs. The state bucket and the evidence bucket (versioned, uniform bucket-level access, no public access). Service accounts `clanker-observer`, `clanker-planner`, `clanker-debugger`, `clanker-applier-<layer>` and `clanker-node`. The IAM bindings and the IAM deny policy `clanker-agent-guard` |
| `10-network` | A custom-mode VPC network. One regional subnet with secondary ranges `pods` and `services`. A Cloud Router and Cloud NAT. A private services access range and connection for Cloud SQL and Filestore. One reserved regional external IP address. An Artifact Registry Docker repository |
| `20-cluster` | A GKE Standard cluster with private nodes and a public control plane endpoint. One node pool. Workload Identity. The Filestore CSI driver add-on. The cluster is zonal when `zone_count` in `10-network` is `1`, and regional when it is larger |
| `30-data` | A Cloud SQL for PostgreSQL instance (`db-f1-micro`, private IP only, daily backups) and the password of the `postgres` user in Secret Manager. A Filestore `BASIC_HDD` instance, the `StorageClass` `clanker-rwx` and one `ReadWriteMany` `PersistentVolume`. A backup bucket |
| `40-platform` | Nothing on Google Cloud. It calls `modules/shared/platform` with the reserved IP address as `public_address`. That module installs Envoy Gateway and the Gateway of the platform, and a Job in the cluster makes the database and the role of the application in Cloud SQL |
| `50-app` | Nothing on Google Cloud. It calls `modules/shared/app` |

Layers `30-data` and `40-platform` configure the `kubernetes` provider with the cluster
endpoint and CA certificate from the `20-cluster` state and the access token from
`data.google_client_config`. Layers `40-platform` and `50-app` configure the `helm` provider
in the same way. Layer `20-cluster` makes no Kubernetes object and has no `kubernetes` provider.

### Additions to the contract

| Layer | Addition | Reason |
|---|---|---|
| All | Inputs `project_id`, `region` | Provider configuration |
| `00-bootstrap` | Input `grant_windows` | See "Agent identities" |
| `00-bootstrap` | Outputs `clanker_ttl`, `node_service_account` | Layers 10 to 30 read the label value. Layer 20 reads the node account |
| `10-network` | Outputs `zones`, `pods_range_name`, `services_range_name`, `private_services_range_name`, `public_address` | Layers 20, 30 and 40 read them |
| `10-network` | `network_cidr` must be /18 or larger | The module divides it into node, pod, service and private services ranges |
| `20-cluster` | Input `deletion_protection`, output `cluster_location` | An environment with a TTL must be destroyable |
| `20-cluster` | `kubernetes_version` must be `1.36` or later | The cluster is made with no default node pool. See "What is validated and what is not" |
| `20-cluster` | `node_image_version` is a full GKE version | On GKE the node version sets the node OS image |
| `30-data` | `postgres_version` uses only the major version | Cloud SQL selects the minor version |
| `30-data` | `shared_fs_capacity_gb` must be 1024 or more | Minimum of the Filestore `BASIC_HDD` tier |
| `30-data` | Input `deletion_protection`, output `shared_fs_capacity_gb` | Layer 40 gives the capacity to the shared module |

`public_subnet_ids` is an empty list. A passthrough Network Load Balancer has no subnet.

### Labels

The `google` provider puts `clanker_env`, `clanker_layer` and `clanker_ttl` on each resource
through `default_labels`. The node VMs and the Cloud SQL instance do not get provider default
labels, so their modules set the labels directly. A label value cannot hold a timestamp, so
`clanker_ttl` is the lifetime: `72h` for `ttl_hours = 72`, and `none` for `0`.

## Agent identities

| Identity | Access |
|---|---|
| `observer` | Standing viewer roles for Compute Engine, GKE, Cloud SQL, Filestore, Artifact Registry, Logging and Monitoring. No role that reads a secret value or the state |
| `planner` | The `observer` roles, `roles/storage.objectViewer` on the state bucket, and read access to IAM policies and Secret Manager. The state contains secret values, so this identity can read them |
| `applier-<layer>` | Admin roles for the services of its layer and `roles/storage.objectUser` on the state bucket. Each binding has an IAM Condition |
| `debugger` | `roles/container.clusterViewer` with an IAM Condition. Kubernetes RBAC must give the namespace access. These layers do not create that RBAC |

The role lists are in `platform/modules/gcp/agent-identities/main.tf`.

### How an applier gets access

1. Only `broker_principal` has `roles/iam.serviceAccountTokenCreator` on the agent service
   accounts. The binding is authoritative for that role, so OpenTofu removes other members.
2. Each role binding of an applier and of the debugger has this IAM Condition:
   `request.time >= timestamp("<not_before>") && request.time < timestamp("<expires>")`.
3. The times come from the `grant_windows` input of `00-bootstrap`. An identity with no entry
   gets a window that starts and ends at `1970-01-01T00:00:00Z`. IAM then refuses each request,
   also when the broker mints a token for the identity.
4. To open a window, add the identity to `grant_windows` with the `not_before` and `expires`
   times of the approved grant and apply `00-bootstrap`. After `expires`, IAM refuses requests
   again without a second apply. Remove the entry later to keep the file clean.
5. The lifetime of the impersonation token (one hour at most by default) is the second limit.

Example:

```hcl
grant_windows = {
  "applier-30-data" = {
    not_before = "2026-11-21T02:00:00Z"
    expires    = "2026-11-21T03:00:00Z"
  }
}
```

A change to `grant_windows` is an IAM change in `00-bootstrap`. The broker has no permission
to make it. The identity that applies it must be decided in the control plane design.

### Guard

The deny policy `clanker-agent-guard` is attached to the project. It applies to all agent
service accounts and overrides each allow binding. It denies these operations: set the project
IAM policy, delete the project, create, update or delete a custom role, create, delete or set
the IAM policy of a service account, create a service account key, and mint or sign a
credential for a service account. As a result, an applier cannot give itself standing access
and cannot use a different service account.

The administrator who applies `00-bootstrap` needs `roles/iam.denyAdmin`, which Google Cloud
grants at the organization level.

## Sizes and cost estimate

This is an estimate. It uses list prices for `us-central1` from memory, without discounts,
free tiers or taxes. Get current prices from the Google Cloud pricing calculator before you
use these numbers for a decision.

| Item | Size in `terraform.tfvars` | Per hour (USD) | Per month, 730 h (USD) |
|---|---|---|---|
| GKE cluster management fee | 1 zonal cluster | 0.10 | 73 |
| GKE nodes | 2 x `e2-standard-2` | 0.134 | 98 |
| Node boot disks | 2 x 30 GB `pd-balanced` | 0.008 | 6 |
| Cloud SQL | `db-f1-micro`, 10 GB SSD, 7 backups | 0.015 | 11 |
| Filestore | `BASIC_HDD`, 1024 GiB | 0.224 | 164 |
| Load balancer forwarding rule and IP address | 1 rule | 0.025 | 18 |
| Cloud NAT | 1 gateway, 2 VMs, low traffic | 0.005 | 4 |
| Buckets, Artifact Registry, Secret Manager, logs | Small volumes | 0.005 | 4 |
| **Total** | | **about 0.52** | **about 380** |

Notes:

- Filestore is the largest item. `BASIC_HDD` has a 1 TiB minimum and is the cheapest tier.
- The GKE free tier pays the management fee of one zonal cluster for each billing account.
  If it applies, the total is about 305 USD for each month.
- Network egress and load generator traffic are not in the table.

## What is validated and what is not

Validated, with no cloud credentials:

- `tofu fmt -check -recursive platform`
- `tofu init -backend=false` and `tofu validate` in each of the six roots, with no errors and
  no warnings
- Each `terraform.tfvars` sets no unknown variable. It sets each variable that has no
  default, except `env_name`, `state_location`, `broker_principal`, `project_id` and
  `region`: the job gives them, because a value in the file replaces a `TF_VAR_*` value

Not done:

- `tofu validate` does not evaluate variable validation rules. The values in the
  `terraform.tfvars` files were compared with the rules by reading only.
- No destroy was done. The sandbox has a TTL, so test the teardown before you rely on it.

- No `tofu plan` and no `tofu apply`. No resource exists.
- The IAM role lists and the denied permissions are not tested against the real API. A role
  can be too small for its layer, and the deny policy API can refuse a permission name.
- The cluster is made with no default node pool. The provider documents that GKE
  `1.36.4-gke.1469000` and later permit this, so `kubernetes_version` must be `1.36` or later.
  This was not tested.
- The names of GKE versions, the Cloud SQL tier and the Filestore tier are not checked for
  availability in the project and region.
- The conformance suite did not run.
- `modules/shared/platform` and `modules/shared/app` were stubs during validation.
