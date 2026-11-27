# This layer has no Azure resources, so it has no azurerm provider. subscription_id and
# location are inputs of each Azure root and are not used here.

# kubelogin gets an Entra token for the AKS server application, whose ID is the same in
# each tenant. The cluster has no local accounts.
provider "helm" {
  kubernetes = {
    host                   = local.cluster.cluster_endpoint
    cluster_ca_certificate = local.cluster.cluster_ca_certificate

    exec = {
      api_version = "client.authentication.k8s.io/v1"
      command     = "kubelogin"
      args        = local.kubelogin_args
    }
  }
}

locals {
  state_account   = split("/", var.state_location)[0]
  state_container = split("/", var.state_location)[1]
  network         = data.terraform_remote_state.network.outputs
  cluster         = data.terraform_remote_state.cluster.outputs
  platform        = data.terraform_remote_state.platform.outputs

  kubelogin_args = [
    "get-token",
    "--login", var.kubelogin_login,
    "--server-id", "6dae42f8-4368-4678-94ff-3960e28e3630",
  ]
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

data "terraform_remote_state" "platform" {
  backend = "azurerm"

  config = {
    storage_account_name = local.state_account
    container_name       = local.state_container
    key                  = "${var.env_name}/40-platform.tfstate"
    use_azuread_auth     = true
  }
}

module "app" {
  source = "../../../modules/shared/app"

  app                 = local.app
  cloud               = "azure"
  pack_dir            = "${path.root}/../../../../apps/${local.app.name}"
  image_registry      = local.network.image_registry
  app_namespace       = local.platform.app_namespace
  app_secret_name     = local.platform.app_secret_name
  shared_volume_claim = local.platform.shared_volume_claim
  gateway_name        = local.platform.gateway_name
  gateway_namespace   = local.platform.gateway_namespace
  public_address      = local.platform.public_address
  public_port         = local.platform.public_port
  app_domain          = local.platform.app_domain
}
