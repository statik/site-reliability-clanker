provider "azurerm" {
  subscription_id                 = var.subscription_id
  storage_use_azuread             = true
  resource_provider_registrations = "none"

  features {}
}

locals {
  layer           = "20-cluster"
  state_account   = split("/", var.state_location)[0]
  state_container = split("/", var.state_location)[1]
  bootstrap       = data.terraform_remote_state.bootstrap.outputs
  network         = data.terraform_remote_state.network.outputs

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

data "terraform_remote_state" "network" {
  backend = "azurerm"

  config = {
    storage_account_name = local.state_account
    container_name       = local.state_container
    key                  = "${var.env_name}/10-network.tfstate"
    use_azuread_auth     = true
  }
}

module "cluster" {
  source = "../../../modules/azure/cluster"

  env_name            = var.env_name
  resource_group_name = local.bootstrap.workload_resource_group_name
  location            = var.location
  tenant_id           = local.bootstrap.tenant_id
  kubernetes_version  = var.kubernetes_version
  node_count          = var.node_count
  node_size           = var.node_size
  zones               = local.network.zones
  nodes_subnet_id     = local.network.subnet_ids["nodes"]
  cluster_identity_id = local.bootstrap.cluster_identity_id
  kubelet_identity    = local.bootstrap.kubelet_identity
  tags                = local.tags
}
