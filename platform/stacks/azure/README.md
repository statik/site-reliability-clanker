# Azure target

The Azure implementation of `platform/CONTRACT.md`. Each directory is one root module with one
OpenTofu state. The building blocks are in `platform/modules/azure/`.

## Layers

| Layer | Creates |
|---|---|
| `00-bootstrap` | Resource groups `rg-<env>-control` and `rg-<env>`. A storage account with the state container from `state_location` and the `evidence` container. Managed identities `observer`, `planner`, `debugger`, `applier-<layer>` and `broker`, each with a federated credential for the broker. Standing roles for `observer` and `planner`. The conditioned role assignments that limit the broker. The AKS control plane and kubelet identities with their roles. Resource provider registrations |
| `10-network` | Virtual network, subnets `snet-nodes`, `snet-postgres` (delegated) and `snet-private-endpoints`. NAT gateway with one public IP. Private DNS zones for PostgreSQL and Azure Files. Azure Container Registry (Basic) |
| `20-cluster` | AKS (Free tier, one node pool, Azure CNI Overlay, outbound through the NAT gateway). OIDC issuer, workload identity and the Azure Files CSI driver are on. Entra ID with Azure RBAC, no local accounts. A static public IP for the gateway, in the node resource group |
| `30-data` | Key Vault with the PostgreSQL administrator password. PostgreSQL flexible server `B_Standard_B1ms` with private access and the `paperboat` database. Premium FileStorage account with one NFS share, a private endpoint and the `clanker-rwx` `StorageClass`. Storage account with the `postgres-backups` container |
| `40-platform` | Calls `modules/shared/platform` with the static IP. That module installs Envoy Gateway and the Gateway of the platform |
| `50-app` | Calls `modules/shared/app` |

Each root also takes `subscription_id` and `location`. Layers `30-data` to `50-app` take
`kubelogin_login` (default `azurecli`). The runner sets it to `workloadidentity` when it uses a
federated token. `00-bootstrap` takes `broker_issuer`.

## Identities and grants

Azure federated credentials need an issuer and a subject. `broker_issuer` is the OIDC issuer URL
of the broker and `broker_principal` is the subject claim. With one token the broker can sign in
as each identity, but an identity without a role assignment can do nothing.

| Identity | Standing roles |
|---|---|
| `observer` | `Reader` and `Azure Kubernetes Service RBAC Reader` on `rg-<env>` |
| `planner` | `Reader`, `Key Vault Secrets User` and a custom Kubernetes read role on `rg-<env>`. `Storage Blob Data Reader` on the state container |
| `applier-<layer>`, `debugger` | None |
| `broker` | `Role Based Access Control Administrator` with a condition, at three scopes. No other role |

The broker does these steps for a grant:

1. Sign in as `broker`.
2. Create the role assignments in the `grant_roles` output for the identity. Put the work item
   ID in the assignment description.
3. Give the runner a federated token for the identity.
4. Delete the role assignments when the grant expires or the job ends.

The condition on each broker role assignment permits `roleAssignments/write` and
`roleAssignments/delete` only for the listed pairs of principal and role:

| Scope | Identity | Roles the broker can assign |
|---|---|---|
| `rg-<env>` | `applier-10-network`, `applier-20-cluster` | `Contributor` |
| `rg-<env>` | `applier-30-data` | `Contributor`, `Key Vault Secrets Officer`, `Azure Kubernetes Service RBAC Cluster Admin` |
| `rg-<env>` | `applier-40-platform` | `Azure Kubernetes Service RBAC Cluster Admin` |
| `rg-<env>` | `applier-50-app` | `Azure Kubernetes Service RBAC Admin` |
| `rg-<env>` | `debugger` | `Azure Kubernetes Service RBAC Writer` |
| State container | Each applier | `Storage Blob Data Contributor` |
| Subscription | `applier-20-cluster` | `Network Contributor` |

Notes:

- `Contributor` cannot write role assignments. Thus an applier cannot give itself more access.
  The role assignments that the workload needs (kubelet to registry, control plane to network
  and storage) are in `00-bootstrap`.
- The state account and the agent identities are in `rg-<env>-control`. No grant gives a role on
  that group, so an applier cannot change the state account or add a federated credential.
- AKS makes the node resource group `rg-<env>-nodes`, and the static public IP must be in it.
  No scope smaller than the subscription exists before the first apply of `20-cluster`. For the
  first apply the broker assigns `Network Contributor` at the subscription. After that it must
  use the node resource group as the scope. The condition cannot enforce the scope.
- The broker can narrow a scope. For example, it can assign the `debugger` role on one namespace
  (`<cluster_id>/namespaces/<name>`).
- The planner must run `tofu plan -lock=false`, because a state lock is a write.
- The administrator who runs bootstrap needs `Owner` on the subscription and
  `Storage Blob Data Contributor` on the new state container to move the state into it. The
  state account has no shared key access.
- No agent has a role on the `evidence` container. The control plane needs one before it can
  write evidence.

## Cost estimate

This is an estimate from public list prices for pay-as-you-go in a US region. It is not a quote.
Data transfer and log volume are not included.

| Item | Size | USD per month |
|---|---|---|
| AKS control plane | Free tier | 0 |
| Nodes | 2 x `Standard_B2ms` | 121 |
| Node OS disks | 2 x 32 GiB premium SSD | 11 |
| NAT gateway | 1, plus 0.045 USD per GB | 33 |
| Public IP addresses | 2 x Standard static | 7 |
| Load balancer | Standard, up to 5 rules | 18 |
| PostgreSQL flexible server | `B_Standard_B1ms`, 32 GiB | 17 |
| Azure Files | Premium, 100 GiB provisioned | 16 |
| Private endpoint | 1 | 7 |
| Container Registry | Basic | 5 |
| Key Vault, storage accounts, private DNS zones | Small | 2 |
| **Total** | | **About 240 per month, about 0.33 per hour** |

## What is validated

- `tofu fmt -check -recursive platform` passes.
- `tofu init -backend=false` passes for each of the six roots.
- `tofu validate` passes for each of the six roots, with no warnings.

Nothing was planned or applied, because no Azure credentials were available. These items need a
real apply:

- The syntax of the role assignment conditions and the wildcard data action of the custom
  planner role.
- The Kubernetes version, VM size and PostgreSQL version that the region and subscription offer.
- Whether Azure accepts `postgres` as the administrator login.
- The NFS mount through the private endpoint, and the `StorageClass` parameters.
- That the Envoy `Service` in `modules/shared/platform` gets the static IP. The module must set
  `loadBalancerIP` (or the equivalent Envoy Gateway setting) to `public_address`.
- `kubelogin` must be on the `PATH` of the runner.

Known limits:

- The `.terraform.lock.hcl` files have hashes for `darwin_arm64` only. Run
  `tofu providers lock -platform=linux_amd64 -platform=linux_arm64` in each root before the
  runner image uses them.
- AKS cannot pin a node image in the cluster resource. The `node_image_version` input is not
  applied. The `node_image_version` output reports the image that runs now. A node image change
  is `az aks nodepool upgrade --node-image-only`.
- `postgres_version` uses the major part only. The output reports the major version.
- `30-data` has the sensitive output `postgres_admin_password`, because `40-platform` can read a
  lower layer through remote state only. The password is also in Key Vault.
