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
  cluster         = data.terraform_remote_state.cluster.outputs
  data            = data.terraform_remote_state.data.outputs

  kubelogin_args = [
    "get-token",
    "--login", var.kubelogin_login,
    "--server-id", "6dae42f8-4368-4678-94ff-3960e28e3630",
  ]
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

data "terraform_remote_state" "data" {
  backend = "azurerm"

  config = {
    storage_account_name = local.state_account
    container_name       = local.state_container
    key                  = "${var.env_name}/30-data.tfstate"
    use_azuread_auth     = true
  }
}

module "platform" {
  source = "../../../modules/shared/platform"

  env_name                = var.env_name
  app_namespace           = var.app_namespace
  public_port             = var.public_port
  postgres_host           = local.data.postgres_host
  postgres_port           = local.data.postgres_port
  postgres_database       = local.data.postgres_database
  postgres_admin_password = local.data.postgres_admin_password
  rwx_storage_class       = local.data.rwx_storage_class
  shared_fs_capacity_gb   = local.data.shared_fs_capacity_gb
  gateway_class           = "envoy"
  public_address          = local.cluster.public_address
}
