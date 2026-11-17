# A human administrator applies this layer one time. It is the only layer that registers
# resource providers and writes standing role assignments.
provider "azurerm" {
  subscription_id                 = var.subscription_id
  storage_use_azuread             = true
  resource_provider_registrations = "none"
  resource_providers_to_register = [
    "Microsoft.Compute",
    "Microsoft.ContainerRegistry",
    "Microsoft.ContainerService",
    "Microsoft.DBforPostgreSQL",
    "Microsoft.KeyVault",
    "Microsoft.ManagedIdentity",
    "Microsoft.Network",
    "Microsoft.Storage",
  ]

  features {}
}

locals {
  layer           = "00-bootstrap"
  state_account   = split("/", var.state_location)[0]
  state_container = split("/", var.state_location)[1]

  tags = {
    clanker_env   = var.env_name
    clanker_layer = local.layer
    clanker_ttl   = tostring(var.ttl_hours)
  }
}

# State, evidence and the agent identities. No agent can get a role on this group.
resource "azurerm_resource_group" "control" {
  name     = "rg-${var.env_name}-control"
  location = var.location
  tags     = local.tags
}

# Layers 10 to 50 put their resources here. Grants give roles on this group only.
resource "azurerm_resource_group" "workload" {
  name     = "rg-${var.env_name}"
  location = var.location
  tags     = local.tags
}

module "state_backend" {
  source = "../../../modules/azure/state-backend"

  resource_group_name     = azurerm_resource_group.control.name
  location                = azurerm_resource_group.control.location
  storage_account_name    = local.state_account
  state_container_name    = local.state_container
  evidence_container_name = "evidence"
  tags                    = local.tags
}

module "identities" {
  source = "../../../modules/azure/identities"

  env_name                     = var.env_name
  location                     = azurerm_resource_group.control.location
  subscription_id              = var.subscription_id
  control_resource_group_name  = azurerm_resource_group.control.name
  workload_resource_group_name = azurerm_resource_group.workload.name
  workload_resource_group_id   = azurerm_resource_group.workload.id
  state_container_id           = module.state_backend.state_container_id
  broker_issuer                = var.broker_issuer
  broker_principal             = var.broker_principal
  tags                         = local.tags
}
