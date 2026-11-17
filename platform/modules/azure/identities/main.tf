terraform {
  required_providers {
    azurerm = {
      source = "hashicorp/azurerm"
    }
  }
}

variable "env_name" {
  type        = string
  description = "Environment name. Identity names start with id-<env_name>."
}

variable "location" {
  type        = string
  description = "Azure region."
}

variable "subscription_id" {
  type        = string
  description = "Subscription ID. Built-in roles are read at this scope."
}

variable "control_resource_group_name" {
  type        = string
  description = "Resource group for the agent and broker identities. No agent can get a role on it."
}

variable "workload_resource_group_name" {
  type        = string
  description = "Resource group for layers 10 to 50. The AKS identities are here."
}

variable "workload_resource_group_id" {
  type        = string
  description = "ID of the workload resource group. Most role assignments use it as scope."
}

variable "state_container_id" {
  type        = string
  description = "Resource Manager ID of the state container."
}

variable "broker_issuer" {
  type        = string
  description = "OIDC issuer URL of the broker."
}

variable "broker_principal" {
  type        = string
  description = "Subject claim of the broker tokens."
}

variable "tags" {
  type        = map(string)
  description = "Tags for each resource."
}

locals {
  layers       = ["10-network", "20-cluster", "30-data", "40-platform", "50-app"]
  appliers     = [for layer in local.layers : "applier-${layer}"]
  agents       = concat(["observer", "planner", "debugger"], local.appliers)
  subscription = "/subscriptions/${var.subscription_id}"

  token_audience = "api://AzureADTokenExchange"

  # Roles that an identity has at all times. Appliers and the debugger have none.
  standing_roles = {
    observer-reader = {
      identity = "observer"
      role     = "Reader"
      scope    = var.workload_resource_group_id
    }
    observer-kubernetes = {
      identity = "observer"
      role     = "Azure Kubernetes Service RBAC Reader"
      scope    = var.workload_resource_group_id
    }
    planner-reader = {
      identity = "planner"
      role     = "Reader"
      scope    = var.workload_resource_group_id
    }
    planner-secrets = {
      identity = "planner"
      role     = "Key Vault Secrets User"
      scope    = var.workload_resource_group_id
    }
    planner-state = {
      identity = "planner"
      role     = "Storage Blob Data Reader"
      scope    = var.state_container_id
    }
  }

  # Roles that the broker may assign for the lifetime of a grant, and may delete again.
  # Each entry becomes one conditioned role assignment for the broker at that scope.
  grantable = {
    workload = {
      scope = var.workload_resource_group_id
      roles = {
        "applier-10-network" = ["Contributor"]
        "applier-20-cluster" = ["Contributor"]
        "applier-30-data" = [
          "Contributor",
          "Key Vault Secrets Officer",
          "Azure Kubernetes Service RBAC Cluster Admin",
        ]
        "applier-40-platform" = ["Azure Kubernetes Service RBAC Cluster Admin"]
        "applier-50-app"      = ["Azure Kubernetes Service RBAC Admin"]
        "debugger"            = ["Azure Kubernetes Service RBAC Writer"]
      }
    }
    state = {
      scope = var.state_container_id
      roles = { for applier in local.appliers : applier => ["Storage Blob Data Contributor"] }
    }
    # AKS makes the node resource group, so no narrower scope exists before the first apply.
    # The broker must assign this role on the node resource group when that group exists.
    node-resource-group = {
      scope = local.subscription
      roles = {
        "applier-20-cluster" = ["Network Contributor"]
      }
    }
  }

  grantable_role_names = toset(flatten([
    for guard in local.grantable : [for roles in guard.roles : roles]
  ]))

  guard_conditions = {
    for key, guard in local.grantable : key => join(" AND ", [
      for action, source in { write = "@Request", delete = "@Resource" } : format(
        "((!(ActionMatches{'Microsoft.Authorization/roleAssignments/%s'})) OR (%s))",
        action,
        join(" OR ", [
          for identity, roles in guard.roles : format(
            "(%s[Microsoft.Authorization/roleAssignments:PrincipalId] ForAnyOfAnyValues:GuidEquals {%s} AND %s[Microsoft.Authorization/roleAssignments:RoleDefinitionId] ForAnyOfAnyValues:GuidEquals {%s})",
            source,
            azurerm_user_assigned_identity.agent[identity].principal_id,
            source,
            join(", ", [for role in roles : basename(data.azurerm_role_definition.grantable[role].id)]),
          )
        ]),
      )
    ])
  }
}

resource "azurerm_user_assigned_identity" "agent" {
  for_each = toset(local.agents)

  name                = "id-${var.env_name}-${each.key}"
  resource_group_name = var.control_resource_group_name
  location            = var.location
  tags                = var.tags
}

resource "azurerm_federated_identity_credential" "agent" {
  for_each = azurerm_user_assigned_identity.agent

  name                      = "broker"
  user_assigned_identity_id = each.value.id
  audience                  = [local.token_audience]
  issuer                    = var.broker_issuer
  subject                   = var.broker_principal
}

resource "azurerm_user_assigned_identity" "broker" {
  name                = "id-${var.env_name}-broker"
  resource_group_name = var.control_resource_group_name
  location            = var.location
  tags                = var.tags
}

resource "azurerm_federated_identity_credential" "broker" {
  name                      = "broker"
  user_assigned_identity_id = azurerm_user_assigned_identity.broker.id
  audience                  = [local.token_audience]
  issuer                    = var.broker_issuer
  subject                   = var.broker_principal
}

resource "azurerm_role_assignment" "standing" {
  for_each = local.standing_roles

  scope                            = each.value.scope
  role_definition_name             = each.value.role
  principal_id                     = azurerm_user_assigned_identity.agent[each.value.identity].principal_id
  principal_type                   = "ServicePrincipal"
  skip_service_principal_aad_check = true
  description                      = "Clanker ${var.env_name}: standing access of ${each.value.identity}."
}

# Helm keeps release records in Kubernetes secrets, so `tofu plan` must read secrets.
# No built-in read-only AKS role permits that.
resource "azurerm_role_definition" "kubernetes_plan_reader" {
  name        = "clanker-${var.env_name}-kubernetes-plan-reader"
  scope       = var.workload_resource_group_id
  description = "Read each Kubernetes object, secrets included, so that the planner can refresh state."

  permissions {
    data_actions = ["Microsoft.ContainerService/managedClusters/*/read"]
  }

  assignable_scopes = [var.workload_resource_group_id]
}

resource "azurerm_role_assignment" "planner_kubernetes" {
  scope                            = var.workload_resource_group_id
  role_definition_id               = azurerm_role_definition.kubernetes_plan_reader.role_definition_resource_id
  principal_id                     = azurerm_user_assigned_identity.agent["planner"].principal_id
  principal_type                   = "ServicePrincipal"
  skip_service_principal_aad_check = true
  description                      = "Clanker ${var.env_name}: standing access of planner."
}

data "azurerm_role_definition" "grantable" {
  for_each = local.grantable_role_names

  name  = each.key
  scope = local.subscription
}

# The guard rail. The broker can write and delete role assignments, but only for the
# principal and role pairs in local.grantable. It has no other permission.
resource "azurerm_role_assignment" "broker_guard" {
  for_each = local.grantable

  scope                            = each.value.scope
  role_definition_name             = "Role Based Access Control Administrator"
  principal_id                     = azurerm_user_assigned_identity.broker.principal_id
  principal_type                   = "ServicePrincipal"
  skip_service_principal_aad_check = true
  condition_version                = "2.0"
  condition                        = local.guard_conditions[each.key]
  description                      = "Clanker ${var.env_name}: broker may assign only the grant roles (${each.key})."
}

# AKS identities. The human administrator makes their role assignments here, so that no
# applier needs permission to write role assignments.
resource "azurerm_user_assigned_identity" "cluster" {
  name                = "id-${var.env_name}-aks-control-plane"
  resource_group_name = var.workload_resource_group_name
  location            = var.location
  tags                = var.tags
}

resource "azurerm_user_assigned_identity" "kubelet" {
  name                = "id-${var.env_name}-aks-kubelet"
  resource_group_name = var.workload_resource_group_name
  location            = var.location
  tags                = var.tags
}

resource "azurerm_role_assignment" "cluster" {
  for_each = {
    "Network Contributor"         = var.workload_resource_group_id
    "Storage Account Contributor" = var.workload_resource_group_id
    "Managed Identity Operator"   = azurerm_user_assigned_identity.kubelet.id
  }

  scope                            = each.value
  role_definition_name             = each.key
  principal_id                     = azurerm_user_assigned_identity.cluster.principal_id
  principal_type                   = "ServicePrincipal"
  skip_service_principal_aad_check = true
  description                      = "Clanker ${var.env_name}: AKS control plane."
}

resource "azurerm_role_assignment" "kubelet_pull" {
  scope                            = var.workload_resource_group_id
  role_definition_name             = "AcrPull"
  principal_id                     = azurerm_user_assigned_identity.kubelet.principal_id
  principal_type                   = "ServicePrincipal"
  skip_service_principal_aad_check = true
  description                      = "Clanker ${var.env_name}: nodes pull images from the environment registry."
}

output "identities" {
  value       = { for name, identity in azurerm_user_assigned_identity.agent : name => identity.id }
  description = "Agent name to managed identity resource ID."
}

output "identity_client_ids" {
  value       = { for name, identity in azurerm_user_assigned_identity.agent : name => identity.client_id }
  description = "Agent name to client ID. The broker uses it in the token exchange."
}

output "identity_principal_ids" {
  value       = { for name, identity in azurerm_user_assigned_identity.agent : name => identity.principal_id }
  description = "Agent name to object ID. The broker uses it in role assignments."
}

output "broker_identity" {
  value = {
    id           = azurerm_user_assigned_identity.broker.id
    client_id    = azurerm_user_assigned_identity.broker.client_id
    principal_id = azurerm_user_assigned_identity.broker.principal_id
    tenant_id    = azurerm_user_assigned_identity.broker.tenant_id
  }
  description = "Identity that the broker uses to write and delete grant role assignments."
}

output "permission_guards" {
  value       = { for key, guard in azurerm_role_assignment.broker_guard : key => guard.id }
  description = "Guard name to ID of the conditioned role assignment."
}

output "grant_roles" {
  value = {
    for agent in local.agents : agent => flatten([
      for guard in local.grantable : [
        for role in lookup(guard.roles, agent, []) : {
          role               = role
          role_definition_id = data.azurerm_role_definition.grantable[role].id
          scope              = guard.scope
        }
      ]
    ])
  }
  description = "Agent name to the roles that the broker assigns for a grant."
}

output "cluster_identity_id" {
  value = azurerm_user_assigned_identity.cluster.id
}

output "kubelet_identity" {
  value = {
    id           = azurerm_user_assigned_identity.kubelet.id
    client_id    = azurerm_user_assigned_identity.kubelet.client_id
    principal_id = azurerm_user_assigned_identity.kubelet.principal_id
  }
}
