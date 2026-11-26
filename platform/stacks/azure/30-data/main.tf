provider "azurerm" {
  subscription_id                 = var.subscription_id
  storage_use_azuread             = true
  resource_provider_registrations = "none"

  features {}
}

# kubelogin gets an Entra token for the AKS server application, whose ID is the same in
# each tenant. The cluster has no local accounts.
provider "kubernetes" {
  host                   = local.cluster.cluster_endpoint
  cluster_ca_certificate = local.cluster.cluster_ca_certificate

  exec {
    api_version = "client.authentication.k8s.io/v1"
    command     = "kubelogin"
    args = [
      "get-token",
      "--login", var.kubelogin_login,
      "--server-id", "6dae42f8-4368-4678-94ff-3960e28e3630",
    ]
  }
}

locals {
  layer           = "30-data"
  state_account   = split("/", var.state_location)[0]
  state_container = split("/", var.state_location)[1]
  bootstrap       = data.terraform_remote_state.bootstrap.outputs
  network         = data.terraform_remote_state.network.outputs
  cluster         = data.terraform_remote_state.cluster.outputs
  resource_group  = local.bootstrap.workload_resource_group_name

  # These names are global. Storage accounts accept 24 lowercase letters and digits.
  name_suffix  = substr(sha256("${var.subscription_id}/${var.env_name}"), 0, 6)
  compact_name = substr(replace(var.env_name, "-", ""), 0, 16)

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

data "terraform_remote_state" "cluster" {
  backend = "azurerm"

  config = {
    storage_account_name = local.state_account
    container_name       = local.state_container
    key                  = "${var.env_name}/20-cluster.tfstate"
    use_azuread_auth     = true
  }
}

module "key_vault" {
  source = "../../../modules/azure/key-vault"

  name                = "kv-${var.env_name}-${substr(local.name_suffix, 0, 4)}"
  resource_group_name = local.resource_group
  location            = var.location
  tenant_id           = local.bootstrap.tenant_id
  tags                = local.tags
}

module "postgres" {
  source = "../../../modules/azure/postgres"

  name                  = "psql-${var.env_name}-${local.name_suffix}"
  resource_group_name   = local.resource_group
  location              = var.location
  major_version         = split(".", var.postgres_version)[0]
  storage_gb            = var.postgres_storage_gb
  backup_retention_days = var.backup_retention_days
  zone                  = local.network.zones[0]
  delegated_subnet_id   = local.network.subnet_ids["postgres"]
  private_dns_zone_id   = local.network.private_dns_zone_ids["postgres"]
  key_vault_id          = module.key_vault.key_vault_id
  tags                  = local.tags
}

module "shared_files" {
  source = "../../../modules/azure/shared-files"

  storage_account_name        = "fs${local.compact_name}${local.name_suffix}"
  resource_group_name         = local.resource_group
  location                    = var.location
  capacity_gb                 = var.shared_fs_capacity_gb
  private_endpoints_subnet_id = local.network.subnet_ids["private-endpoints"]
  private_dns_zone_id         = local.network.private_dns_zone_ids["file"]
  storage_class_name          = "clanker-rwx"
  tags                        = local.tags
}

module "backup_storage" {
  source = "../../../modules/azure/backup-storage"

  storage_account_name = "bk${local.compact_name}${local.name_suffix}"
  resource_group_name  = local.resource_group
  location             = var.location
  tags                 = local.tags
}
