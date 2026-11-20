# Platform contract

This file is the interface between the cloud-specific OpenTofu code and everything above it.
Four targets implement it: `aws`, `gcp`, `azure` and `local`. The control plane, the policy
rules and the conformance suite read only the names in this file.

## 1. Directory layout

```
platform/
  CONTRACT.md
  modules/
    <cloud>/<capability>/     cloud-specific building blocks (aws, gcp, azure, local)
    shared/platform/          cloud-agnostic layer 40, Kubernetes and Helm providers only
    shared/app/               cloud-agnostic layer 50, Kubernetes and Helm providers only
  stacks/
    <cloud>/<layer>/          one root module per cloud and layer
```

Layers are `00-bootstrap`, `10-network`, `20-cluster`, `30-data`, `40-platform`, `50-app`.
Each root module has one OpenTofu state. A root module contains:

| File | Content |
|---|---|
| `versions.tf` | `required_version`, `required_providers` with exact versions, the backend block |
| `variables.tf` | The inputs in section 3 |
| `main.tf` | Provider configuration and module calls |
| `outputs.tf` | The outputs in section 3 |
| `terraform.tfvars` | The values for this environment. The agent edits this file |

## 2. Rules for every root module

1. Use OpenTofu `>= 1.10`. Pin each provider to an exact version.
2. Declare the common inputs `env_name` (string) and `state_location` (string).
3. Read the outputs of lower layers with the `terraform_remote_state` data source only.
   Do not read a lower layer in any other way.
4. Give each output the exact name and type in section 3. Extra outputs are permitted.
   Mark secret values `sensitive = true`.
5. Put a `clanker_env`, `clanker_layer` and `clanker_ttl` label or tag on each cloud resource
   that accepts labels.
6. Layers `40-platform` and `50-app` call `modules/shared/platform` and `modules/shared/app`.
   The root module configures the `kubernetes` and `helm` providers for its cloud and passes
   nothing cloud-specific into the shared module except the inputs in section 3.
7. Use the smallest instance sizes that pass the conformance suite.

### State backends

| Cloud | Backend | `state_location` meaning | State key |
|---|---|---|---|
| aws | `s3` with `use_lockfile = true` | bucket name | `<env_name>/<layer>.tfstate` |
| gcp | `gcs` | bucket name | prefix `<env_name>/<layer>` |
| azure | `azurerm` | `<storage account>/<container>` | `<env_name>/<layer>.tfstate` |
| local | `local` | absolute directory path | `<state_location>/<layer>.tfstate` |

The backend block in `versions.tf` is empty (`backend "s3" {}`). The runner supplies the
values with `tofu init -backend-config=...`. Layer `00-bootstrap` creates the backend, so its
own state starts local and `clanker bootstrap` migrates it.

### The home of the control plane is not a layer

The setup wizard creates the home of the control plane before any layer exists. That home
is outside this contract. The role that the control plane runs as is the value for
`broker_principal` of `00-bootstrap` on AWS.

## 3. Inputs and outputs for each layer

Types use OpenTofu syntax. "Local" shows how the local target satisfies the row.

### 00-bootstrap

| Input | Type | Meaning |
|---|---|---|
| `env_name` | `string` | Environment name, for example `aws-sandbox` |
| `state_location` | `string` | See the backend table |
| `broker_principal` | `string` | Identity that can assume the agent roles. Empty on local |
| `ttl_hours` | `number` | Lifetime of the environment. `0` means no TTL |

| Output | Type | Meaning | Local |
|---|---|---|---|
| `state_backend` | `map(string)` | Backend configuration for the other layers | `{ path = <dir> }` |
| `evidence_bucket` | `string` | Bucket for audit evidence and backups | Directory path |
| `identities` | `map(string)` | Keys `observer`, `planner`, `debugger`, `applier-10-network` … `applier-50-app`. Values are role ARNs, service account emails or managed identity IDs | Kubernetes service account names |
| `permission_guard` | `string` | ID of the permission boundary, deny policy or assignment condition | Empty |

### 10-network

| Input | Type | Meaning |
|---|---|---|
| `network_cidr` | `string` | Address range for the environment |
| `zone_count` | `number` | Number of zones to use |

| Output | Type | Meaning | Local |
|---|---|---|---|
| `network_id` | `string` | VPC, VPC network or VNet ID | Docker network name |
| `private_subnet_ids` | `list(string)` | Subnets for nodes and data services | `[]` |
| `public_subnet_ids` | `list(string)` | Subnets for load balancers | `[]` |
| `image_registry` | `string` | Registry host and path prefix for Paperboat images | `localhost:5001` registry container |

### 20-cluster

| Input | Type | Meaning |
|---|---|---|
| `kubernetes_version` | `string` | Minor version, for example `1.34` |
| `node_image_version` | `string` | Node OS image version. Empty means the cloud default |
| `node_count` | `number` | Number of worker nodes |
| `node_size` | `string` | Instance type or VM size |

| Output | Type | Meaning | Local |
|---|---|---|---|
| `cluster_name` | `string` | Cluster name | kind cluster name |
| `cluster_endpoint` | `string` | API server URL | kind API URL |
| `cluster_ca_certificate` | `string`, sensitive | PEM, not base64 | From kind |
| `kubeconfig_path` | `string` | Path of a kubeconfig file. Empty on the clouds, which use exec authentication | File in the state directory |
| `kubernetes_version` | `string` | Version that runs now | From kind |
| `node_image_version` | `string` | Node image that runs now | kind node image tag |

### 30-data

| Input | Type | Meaning |
|---|---|---|
| `postgres_version` | `string` | Full version, for example `17.5` |
| `postgres_storage_gb` | `number` | Database storage |
| `shared_fs_capacity_gb` | `number` | Shared file system capacity |
| `backup_retention_days` | `number` | Retention for automatic backups |

| Output | Type | Meaning | Local |
|---|---|---|---|
| `postgres_host` | `string` | Private host name | In-cluster service DNS name |
| `postgres_port` | `number` | Port | `5432` |
| `postgres_database` | `string` | Database name for Paperboat | `paperboat` |
| `postgres_admin_secret_ref` | `string` | Secret store reference for the admin credential | `<namespace>/<secret name>` |
| `postgres_version` | `string` | Version that runs now | Image tag |
| `rwx_storage_class` | `string` | Name of a `StorageClass` that supports `ReadWriteMany` | Static volume class |
| `backup_bucket` | `string` | Bucket or path for database backups | Directory on the node |
| `data_namespace` | `string` | Local target only: the namespace of the data services in the cluster | `paperboat-data` |

### 40-platform

| Input | Type | Meaning |
|---|---|---|
| `app_namespace` | `string` | Namespace for Paperboat. Default `paperboat` |
| `public_port` | `number` | Port of the public listener. Default `80` |

| Output | Type | Meaning | Local |
|---|---|---|---|
| `app_namespace` | `string` | Namespace for Paperboat | Same |
| `gateway_class` | `string` | `GatewayClass` name, or `nodeport` when the target has no Gateway API | `nodeport` |
| `public_address` | `string` | Host name or IP address that serves the application | `localhost:8088` |
| `app_secret_name` | `string` | Kubernetes secret in `app_namespace` with keys `DATABASE_URL` | Same |
| `shared_volume_claim` | `string` | `PersistentVolumeClaim` in `app_namespace`, `ReadWriteMany` | Same |
| `targets` | `map(string)` | Where the application runs. The key `application` has the namespace of its components. The key `data` has the namespace of its data services, or it is empty when they are not in the cluster. The control plane reads the namespaces of a debug session and of the rights of its identities from this output, not from the application pack | `data` is the output `data_namespace` of `30-data` |

### 50-app

| Input | Type | Meaning |
|---|---|---|
| `paperboat_version` | `string` | Release to run, for example `1.1.0` |
| `api_replicas` | `number` | Replicas of `api` |
| `worker_replicas` | `number` | Replicas of `worker` |
| `web_replicas` | `number` | Replicas of `web` |
| `loadgen_enabled` | `bool` | Run synthetic traffic |
| `workload_runtime` | `string` | `kubernetes` or `unikernel`. Only `kubernetes` is implemented |

| Output | Type | Meaning |
|---|---|---|
| `app_url` | `string` | URL that serves Paperboat |
| `paperboat_version` | `string` | Release that runs now |
| `release_revision` | `number` | Helm revision |

## 4. Paperboat chart interface

Layer `50-app` installs the chart in `apps/paperboat/chart` with these values:

| Value | From |
|---|---|
| `image.registry` | `10-network.image_registry` |
| `image.tag` | `paperboat_version` |
| `api.replicas`, `worker.replicas`, `web.replicas` | The matching inputs |
| `loadgen.enabled` | `loadgen_enabled` |
| `database.secretName` | `40-platform.app_secret_name` |
| `sharedVolume.claimName` | `40-platform.shared_volume_claim` |
| `service.type`, `service.nodePort` | `NodePort` and `30080` when `gateway_class` is `nodeport`, otherwise `ClusterIP` |
| `gateway.className` | `40-platform.gateway_class` when it is not `nodeport` |

## 5. Local target specifics

- The runner sets `KIND_EXPERIMENTAL_DOCKER_NETWORK` to the `network_id` output of `10-network`.
- Host port `8088` maps to node port `30080`. Host port `5001` is the image registry.
- The local target has no cloud identities. The broker issues Kubernetes service account
  tokens for the `identities` in `00-bootstrap`.

## 6. Conformance

`conformance/` contains one suite. A target is supported when the suite passes:

1. The cluster API answers.
2. Two pods write and read the same file on the `ReadWriteMany` volume.
3. Postgres accepts a connection from inside the cluster.
4. `public_address` serves Paperboat and `/api/healthz` returns `200`.
5. A database backup completes and restores into a scratch database.
6. A pod reads the application secret.
