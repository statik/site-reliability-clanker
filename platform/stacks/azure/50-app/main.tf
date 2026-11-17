# This layer has no Azure resources, so it has no azurerm provider. subscription_id and
# location are inputs of each Azure root and are not used here.

# kubelogin gets an Entra token for the AKS server application, whose ID is the same in
# each tenant. The cluster has no local accounts.
provider "kubernetes" {
  host                   = local.cluster.cluster_endpoint
  cluster_ca_certificate = local.cluster.cluster_ca_certificate

  exec {
    api_version = "client.authentication.k8s.io/v1"
    command     = "kubelogin"
    args        = local.kubelogin_args
  }
}

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

  paperboat_version   = var.paperboat_version
  api_replicas        = var.api_replicas
  worker_replicas     = var.worker_replicas
  web_replicas        = var.web_replicas
  loadgen_enabled     = var.loadgen_enabled
  workload_runtime    = var.workload_runtime
  image_registry      = local.network.image_registry
  app_namespace       = local.platform.app_namespace
  app_secret_name     = local.platform.app_secret_name
  shared_volume_claim = local.platform.shared_volume_claim
  gateway_class       = local.platform.gateway_class
  public_address      = local.platform.public_address
  chart_path          = abspath("${path.root}/../../../../apps/paperboat/chart")
}
