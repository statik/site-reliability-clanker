output "state_backend" {
  value = {
    resource_group_name  = azurerm_resource_group.control.name
    storage_account_name = module.state_backend.storage_account_name
    container_name       = module.state_backend.state_container_name
    subscription_id      = var.subscription_id
    use_azuread_auth     = "true"
  }
  description = "Backend configuration for the other layers. The keys are azurerm backend arguments."
}

output "evidence_bucket" {
  value       = "${module.state_backend.storage_account_name}/${module.state_backend.evidence_container_name}"
  description = "Evidence location as <storage account>/<container>."
}

output "identities" {
  value       = module.identities.identities
  description = "Agent name to managed identity resource ID."
}

output "permission_guard" {
  value       = module.identities.permission_guards["workload"]
  description = "ID of the conditioned role assignment that limits the broker on the workload resource group."
}

output "permission_guards" {
  value       = module.identities.permission_guards
  description = "IDs of all conditioned role assignments of the broker: workload, state, node-resource-group."
}

output "identity_client_ids" {
  value       = module.identities.identity_client_ids
  description = "Agent name to client ID. The broker uses it in the token exchange."
}

output "identity_principal_ids" {
  value       = module.identities.identity_principal_ids
  description = "Agent name to object ID. The broker uses it in role assignments."
}

output "broker_identity" {
  value       = module.identities.broker_identity
  description = "Identity that the broker uses to write and delete grant role assignments."
}

output "grant_roles" {
  value       = module.identities.grant_roles
  description = "Agent name to the roles and scopes that the broker assigns for a grant."
}

output "tenant_id" {
  value       = module.identities.broker_identity.tenant_id
  description = "Entra tenant of the environment."
}

output "ttl_hours" {
  value       = var.ttl_hours
  description = "Lifetime of the environment. The other layers copy it into the clanker_ttl tag."
}

output "workload_resource_group_name" {
  value       = azurerm_resource_group.workload.name
  description = "Resource group for layers 10 to 50."
}

output "cluster_identity_id" {
  value       = module.identities.cluster_identity_id
  description = "User-assigned identity for the AKS control plane."
}

output "kubelet_identity" {
  value       = module.identities.kubelet_identity
  description = "User-assigned identity for the AKS nodes."
}
