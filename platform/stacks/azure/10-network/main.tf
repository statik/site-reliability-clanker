provider "azurerm" {
  subscription_id                 = var.subscription_id
  storage_use_azuread             = true
  resource_provider_registrations = "none"

  features {}
}

locals {
  layer           = "10-network"
  state_account   = split("/", var.state_location)[0]
  state_container = split("/", var.state_location)[1]
  bootstrap       = data.terraform_remote_state.bootstrap.outputs
  resource_group  = local.bootstrap.workload_resource_group_name

  # Azure Container Registry names are global, and accept letters and digits only.
  name_suffix   = substr(sha256("${var.subscription_id}/${var.env_name}"), 0, 6)
  registry_name = "acr${replace(var.env_name, "-", "")}${local.name_suffix}"

  tags = {
    clanker_env   = var.env_name
    clanker_layer = local.layer
    clanker_ttl   = tostring(local.bootstrap.ttl_hours)
  }
}

data "terraform_remote_state" "bootstrap" {
  backend = "azurerm"

  config = {
    storage_account_name = local.state_account
    container_name       = local.state_container
    key                  = "${var.env_name}/00-bootstrap.tfstate"
    use_azuread_auth     = true
  }
}

module "network" {
  source = "../../../modules/azure/network"

  env_name            = var.env_name
  resource_group_name = local.resource_group
  location            = var.location
  network_cidr        = var.network_cidr
  zone_count          = var.zone_count
  tags                = local.tags
}

module "registry" {
  source = "../../../modules/azure/registry"

  name                = local.registry_name
  resource_group_name = local.resource_group
  location            = var.location
  tags                = local.tags
}
