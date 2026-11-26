# The runner supplies the cluster credential in KUBE_CONFIG_PATH. The access broker issues it.
provider "kubernetes" {}
provider "helm" {}

data "terraform_remote_state" "data" {
  backend = "local"
  config  = { path = "${var.state_location}/30-data.tfstate" }
}

locals {
  data_outputs = data.terraform_remote_state.data.outputs
  admin_ref    = split("/", local.data_outputs.postgres_admin_secret_ref)
}

data "kubernetes_secret_v1" "postgres_admin" {
  metadata {
    namespace = local.admin_ref[0]
    name      = local.admin_ref[1]
  }
}

module "platform" {
  source = "../../../modules/shared/platform"

  env_name                = var.env_name
  app_name                = var.app_name
  workloads_namespace     = var.workloads_namespace
  public_port             = var.public_port
  postgres_host           = local.data_outputs.postgres_host
  postgres_port           = local.data_outputs.postgres_port
  postgres_sslmode        = local.data_outputs.postgres_sslmode
  postgres_admin_password = data.kubernetes_secret_v1.postgres_admin.data["password"]
  rwx_storage_class       = local.data_outputs.rwx_storage_class
  shared_fs_capacity_gb   = var.shared_fs_claim_gb
  node_port               = 30080
  public_address          = "localhost:${var.public_port}"
  collector_endpoint      = var.collector_endpoint
  collector_token         = var.collector_token

  # Public DNS answers every name under this domain with 127.0.0.1, where the host port
  # serves the gateway. CoreDNS answers them in the cluster with the Service of Envoy.
  app_domain          = "127-0-0-1.sslip.io"
  rewrite_cluster_dns = true
}
