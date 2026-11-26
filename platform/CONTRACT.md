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
    shared/app/               cloud-agnostic layer 50, Helm and random providers only
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
| `app.tf` | Layer `50-app` only. The variables, the outputs and the local value `app` that come from the manifest of the application pack. The control plane writes it; see `50-app` in section 3 |
| `terraform.tfvars` | The values for this environment. The agent edits this file. On a cloud target it does not set the inputs that the job gives (see "State backends") |

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
   The root module configures the providers of the shared module for its cloud (`kubernetes`
   and `helm` for `40-platform`, `helm` for `50-app`) and passes nothing cloud-specific
   into the shared module except the inputs in section 3.
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

- On AWS, `clanker bootstrap --cloud aws` applies `00-bootstrap` in a copy below the data
  directory, with a local backend override and its inputs as `-var`. Then it moves the state
  to `<env_name>/00-bootstrap.tfstate` in the bucket `clanker-<env_name>-state-<account>`
  with `tofu init -migrate-state -force-copy`. `clanker destroy --cloud aws` reads the state
  of every layer first, destroys the layers with resources from `50-app` down, then moves
  the state of `00-bootstrap` back to a local file and destroys it.
- On a cloud target the job carries the backend settings. The runner passes them to
  `tofu init` and, when the settings have a `bucket`, sets `TF_VAR_state_location` to it.
  A job on a cloud target without backend settings fails before `tofu` runs. On AWS the
  settings must be exactly `bucket`, `key`, `region`, `use_lockfile = "true"` and
  `encrypt = "true"`, so no setting of the local backend reaches an `s3` backend.
- On the local target the job has no backend settings. The runner passes
  `-backend-config=path=<state directory>/<layer>.tfstate` and sets `TF_VAR_state_location`
  to the state directory.
- After an apply the runner reads the outputs with `tofu output -json`, so they come from
  the backend of the job. It leaves out the sensitive outputs.
- A value in `terraform.tfvars` replaces a `TF_VAR_*` value; only `-var` replaces the file.
  So on a cloud target `terraform.tfvars` does not set `env_name`, `state_location` or
  `broker_principal`, and does not set the inputs that name the region or the account
  (`region`, `location`, `project_id`, `subscription_id`, `broker_issuer`). The job must
  give them.
- On AWS the control plane makes the jobs of layers `10-network` to `50-app`. Their backend
  settings are the output `state_backend` of `00-bootstrap` with
  `key = <env_name>/<layer>.tfstate`. They get `TF_VAR_env_name`, `TF_VAR_state_location`
  (the bucket of `state_backend`) and `TF_VAR_region` (its region). The control plane reads
  `state_backend` from the setting `CLANKER_AWS_BOOTSTRAP`. On a cloud the first deployment
  starts at `10-network`, and the control plane refuses a change to `00-bootstrap`.
- On AWS the inputs that belong to one account (`existing_network_id`,
  `existing_private_subnet_ids`, `image_registry`, `instance_tags`,
  `operator_principal_arn`) are not in a file of this repository. The setting
  `CLANKER_AWS_LAYER_VARIABLES` holds them as a JSON object from the name of a variable to a
  string, a list of strings or a map of strings. The jobs of layers `10-network` to `50-app`
  get each one as `TF_VAR_<name>`. The control plane refuses a name that no layer declares,
  that a `terraform.tfvars` file sets, or that it sets itself.

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
| `identities` | `map(string)` | Keys `observer`, `planner`, `debugger`, `applier-10-network` … `applier-50-app`, and on AWS `runbook`. Values are role ARNs, service account emails or managed identity IDs | Kubernetes service account names |
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
| `postgres_sslmode` | `string` | libpq `sslmode` for connections to Postgres: `require` on aws, gcp and azure | `disable`: the local Postgres has no TLS |
| `postgres_admin_secret_ref` | `string` | Secret store reference for the admin credential | `<namespace>/<secret name>` |
| `postgres_version` | `string` | Version that runs now | Image tag |
| `rwx_storage_class` | `string` | Name of a `StorageClass` that supports `ReadWriteMany` | Static volume class |
| `backup_bucket` | `string` | Bucket or path for database backups | Directory on the node |
| `data_namespace` | `string` | Local target only: the namespace of the data services in the cluster | `clanker-data` |

This layer makes the server and its admin user `postgres`, and no database for the
application. `40-platform` makes that database.

### 40-platform

| Input | Type | Meaning |
|---|---|---|
| `app_name` | `string` | Name of the application pack, for example `paperboat`. Required. The namespace, the database role, the database, the secret `<name>-db` and the claim `<name>-shared` have this name |
| `workloads_namespace` | `bool` | Make the namespace `<name>-workloads` for the pods that the application starts, such as the workspaces of Coder. Default `false`. The control plane writes the line from `deploy.workloads_namespace` of the pack |
| `public_port` | `number` | Port of the public listener. Default `80` |
| `collector_endpoint` | `string` | Address of the control plane for the data of the collector, for example `http://host:7070/otlp`. Default empty: no collector |
| `collector_token` | `string`, sensitive | Token that the collector sends to the control plane. Default empty |

| Output | Type | Meaning | Local |
|---|---|---|---|
| `app_namespace` | `string` | Namespace of the application: `app_name`. It has the label `clanker.dev/application=true` | Same |
| `gateway_name` | `string` | Name of the Gateway that the routes of the application attach to | `clanker` |
| `gateway_namespace` | `string` | Namespace of the Gateway | `clanker-gateway` |
| `public_address` | `string` | Host name or IP address that serves the application | `localhost:8088` |
| `public_port` | `number` | Port of the public listener: the input `public_port` | `8088` |
| `app_domain` | `string` | Domain of the host names of the applications. A route with `hostname` gets `<name>.<app_domain>`. Empty: no application has a host name. The clouds have none yet: a private DNS zone and a certificate for each cloud come later, after the owner approves the cost | `127-0-0-1.sslip.io` |
| `app_secret_name` | `string` | Kubernetes secret `<name>-db` in `app_namespace` with the credential of the database role of the application. See below | Same |
| `shared_volume_claim` | `string` | `PersistentVolumeClaim` `<name>-shared` in `app_namespace`, `ReadWriteMany` | Same |
| `targets` | `map(string)` | Where the application runs. The key `application` has the namespace of its components. The key `data` has the namespace of its data services, or it is empty when they are not in the cluster. The key `workloads` has the namespace `<name>-workloads`, or it is empty when `workloads_namespace` is `false`. The control plane reads the namespaces of a debug session and of the rights of its identities from this output, not from the application pack | `data` is the output `data_namespace` of `30-data` |
| `collector_namespace` | `string` | Namespace of the OpenTelemetry Collector that sends the state of the workloads in `app_namespace` to the control plane. Empty when `collector_endpoint` is empty | `clanker-observe` when the control plane sets `collector_endpoint` |

The control plane gives `collector_endpoint` and `collector_token` to the runner as
`TF_VAR_collector_endpoint` and `TF_VAR_collector_token`; they are not in `terraform.tfvars`.

The entry point is the same on every target, the local target too:

- The layer installs Envoy Gateway with one release. A second release installs the chart
  `modules/shared/platform/gateway` after it: the EnvoyProxy `clanker`, the GatewayClass
  `envoy` and the Gateway `gateway_name` in `gateway_namespace`. A `kubernetes_manifest`
  for these kinds cannot be planned on a new cluster, because their CRDs come with the
  first release.
- The Gateway has one HTTP listener on `public_port`. It admits routes only from
  namespaces with the label `clanker.dev/application=true`.
- Envoy Gateway makes the Service `clanker-envoy` in `envoy-gateway-system`. On a cloud it
  is a `LoadBalancer` Service. When the root gives no `public_address` (aws), the shared
  module reads the address of the load balancer from this Service once, after the release
  of the Gateway. Nothing waits for the address: the aws root fails the apply when the
  Service has none yet.
- The ClusterRole `clanker-httproute-admin` adds the rights on `httproutes` to the
  ClusterRole `admin`, so the applier of `50-app` can manage the route in its namespace.
- The namespace `<name>-workloads` has no label `clanker.dev/application`, so the Gateway
  admits no route from it, and the collector does not read it. A pod there can have any
  spec, so it is not in `app_namespace`, where it could mount the secret `<name>-db`. Only
  the applier of `50-app` has rights there: the role `admin` on the local target, for the
  Role that the chart of the application makes there. The runbook and debugger identities
  have none, and no runbook or debug session gets this namespace.
- On the local target the public DNS answers every name under `app_domain` with
  `127.0.0.1`. That is the host port of the gateway for the browser and the control plane,
  but not for a pod. So the layer writes the key `Corefile` of the ConfigMap
  `kube-system/coredns` (the module input `rewrite_cluster_dns`): the Corefile of kubeadm
  with one rewrite, which answers each name under `app_domain`, and no other name, with the
  Service of Envoy, under the name of the question. The Service has the port `public_port`
  too, so one URL works from the host and from a pod. CoreDNS reloads the file about a
  minute after the apply. The destroy of the layer takes the key out; the destroy of
  `20-cluster` then deletes the cluster.

The database of the application is the same on every target:

- Only the cluster reaches Postgres on every target, so a Kubernetes Job in the namespace
  `clanker-platform` makes the role and the database with `psql`, as the admin user. The
  admin credential is in a secret in that namespace, never in `app_namespace`.
- The role `<name>` can log in and is not a superuser. It owns the database `<name>`, and
  `PUBLIC` cannot connect to that database. The password is a `random_password` in the
  state of the layer.
- The SQL can run again. It stops when `<name>` is the admin user, a superuser, or the name
  of a database of another role. It grants the role to the admin user before
  `CREATE DATABASE ... OWNER`: from PostgreSQL 16, an admin user that is not a superuser
  (RDS, Cloud SQL, Azure) must be able to `SET ROLE` to the owner.
- The name of the Job ends with a hash of the SQL, the client image, the host, the port, the
  `sslmode`, the name and the password of the role. A change of one of them replaces the Job,
  so it runs again. A rotation of the admin password does not run it again.
- The secret `<name>-db` has `PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`,
  `PGSSLMODE` and `DATABASE_URL` =
  `postgresql://<name>:<password>@<host>:<port>/<name>?sslmode=<postgres_sslmode>`. The
  layer writes it after the Job.

### 50-app

The layer is the same for every pack. The pack brings data, not OpenTofu: its chart, its
values files and a release record for each release (`docs/PACKS.md` B.10). Its deploy files
are in `apps/<name>/` of the platform repository.

The static files of the root (`main.tf`, `variables.tf`, `outputs.tf`, `versions.tf`) have
only the inputs of the environment. The control plane writes `app.tf` from the manifest of
the pack (`internal/applayer`), because OpenTofu needs declared names. It also writes the
lines of the pack in `terraform.tfvars` and the lines `app_name` and `workloads_namespace` of the `terraform.tfvars` of
`40-platform`. A new control plane writes them when it seeds the platform repository. The
files of the example pack are committed, and a Go test fails when they are not what the
control plane writes.

| Input | Type | Meaning |
|---|---|---|
| `<version_variable>` | `string` | Release to run, for example `1.1.0`. It must match `^[0-9]+.[0-9]+.[0-9]+(-[a-z][a-z0-9]*)?$`. Paperboat: `paperboat_version` |
| A variable for each entry of `deploy.settings` | `number` or `bool` | A number has a validation for its bounds. Paperboat: `api_replicas`, `worker_replicas`, `web_replicas` (1 to 5) and `loadgen_enabled` |
| `admin_token` | `string`, sensitive | Local only. The token for fault injection in demo mode. Default empty |

| Output | Type | Meaning |
|---|---|---|
| `<url_output>` | `string` | URL that serves the application. Paperboat: `app_url` |
| `<version_variable>` | `string` | Release that runs now |
| `release_revision` | `number` | Helm revision of the main release |

`modules/shared/app` installs three Helm releases in `app_namespace`, in this order:

1. `<name>-platform`, the chart `modules/shared/app/platform` of the core: the HTTPRoute
   `<name>`, the ConfigMap `<name>-platform` and the Secret `<name>-generated`. It comes
   first, so a chart can read the ConfigMap and the Secret when its pods start.
2. `<name>`, the main chart. The release record of the version selects it: an archive of
   the pack in `apps/<name>/charts/`, whose SHA-256 the layer checks with `filesha256`
   against `chart_sha256`; or a chart in a repository, which the runner fetches at apply;
   or, with no chart in the record, `apps/<name>/chart`. An `oci://` chart is
   `<repository>/<chart_name>@<chart_digest>` with `version = chart_version`, so Helm
   checks that the digest is the digest of that version. Helm does not check the digest
   of a chart in an `https://` repository. When the pack has records and the version has
   none, a precondition stops the plan and names the record to add.
3. `<name>-extras`, the chart `apps/<name>/extras`, when the pack has `deploy.extras`.

Each release has `timeout = deploy.timeout_seconds` (default 300), `wait = true` and
`atomic = false`: Helm never rolls back by itself, because a release can have migrated the
database.

## 4. Chart interface

The main chart gets its values in this order, the last one wins:

1. The defaults of the chart.
2. `apps/<name>/values/values.yaml`, then `values-<cloud>.yaml`, when the pack has them.
3. The `values` of the release record.
4. The values of the platform, below, when the chart is the chart directory of the pack.
5. The settings, with Helm `set`, as `--set <path>=<value>` would set them.

The values of the platform. The extras chart gets them too. A chart that a release record
names, as an archive or in a repository, is a chart of another project and does not get
them: its values schema can refuse a key that it does not know, as the schema of Zero to
JupyterHub does.

| Value | From |
|---|---|
| `platform.name` | The name of the pack |
| `platform.namespace` | `40-platform.app_namespace` |
| `platform.version` | The version variable |
| `platform.url` | The URL output: `http://<name>.<app_domain>:<public_port>` for a route with `hostname` when the target has an `app_domain`, else `http://<public_address>` |
| `platform.hostname` | `<name>.<app_domain>` for a route with `hostname` when the target has an `app_domain`, else empty |
| `platform.imageRegistry` | `10-network.image_registry` |
| `platform.databaseSecret` | `40-platform.app_secret_name`: the Secret `<name>-db` |
| `platform.sharedVolumeClaim` | `40-platform.shared_volume_claim`: the claim `<name>-shared` |
| `platform.generatedSecret` | `<name>-generated`, or empty when the pack has no `deploy.secrets` |
| `platform.adminToken` | `admin_token`, with `set_sensitive`. Empty off demo mode |

A chart of another project cannot read `platform.*`. The values files of the pack connect
it to these objects in `app_namespace`, by name, with `env` and `envFrom`:

| Object | Content |
|---|---|
| Secret `<name>-db` | `PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`, `PGSSLMODE`, `DATABASE_URL` (see `40-platform`) |
| ConfigMap `<name>-platform` | `APP_URL` (`platform.url`), `APP_HOSTNAME` (`platform.hostname`), `APP_WILDCARD_HOST` (`*.<name>.<app_domain>` for a route with `wildcard`, else empty), `APP_VERSION`. The host names have no port |
| Secret `<name>-generated` | One key for each entry of `deploy.secrets`, each with a random value of 32 letters and digits that the state of the layer keeps |
| PersistentVolumeClaim `<name>-shared` | `ReadWriteMany` |

The chart makes no Gateway, no route and no Service of type `NodePort` or `LoadBalancer`.
The HTTPRoute `<name>` of the core attaches the `ClusterIP` Service `deploy.route.service`
on `deploy.route.port` to the Gateway of `40-platform`, with the path prefixes of
`deploy.route.paths` (every path when it has none) and the request timeout
`deploy.route.timeout_seconds` (`0` is no timeout; with none, the default of the gateway).
With `deploy.route.hostname` and an `app_domain`, the route has the host name
`<name>.<app_domain>`, and with `deploy.route.wildcard` also `*.<name>.<app_domain>`;
it then gets only requests for those names. Without them it gets every host name.

Paperboat reads `platform.imageRegistry` and `platform.version` for its image,
`platform.databaseSecret`, `platform.sharedVolumeClaim` and `platform.adminToken`. Its
Service `paperboat-web` (port `8080`) is the route service.

## 5. Local target specifics

- The runner sets `KIND_EXPERIMENTAL_DOCKER_NETWORK` to the `network_id` output of `10-network`.
- Host port `8088` maps to node port `30080`. The Envoy Service is a `NodePort` Service
  with node port `30080` for the listener port `8088`. Host port `5001` is the image
  registry.
- `app_domain` is `127-0-0-1.sslip.io`: the public DNS of sslip.io answers every name
  under it with `127.0.0.1`, and CoreDNS answers them in the cluster with the Envoy Service
  (see `40-platform`). A router or resolver that drops DNS answers with a private address
  also drops these; then a line in the hosts file of the machine is the fallback.
- The local target has no cloud identities. The broker issues Kubernetes service account
  tokens for the `identities` in `00-bootstrap`.

## 6. Conformance

`conformance/` contains one suite. A target is supported when the suite passes:

1. The cluster API answers.
2. Two pods write and read the same file on the `ReadWriteMany` volume.
3. Postgres accepts a connection from inside the cluster.
4. `public_address` serves the application of the pack, and its primary check passes.
5. A database backup completes and restores into a scratch database.
6. A pod reads the application secret.
