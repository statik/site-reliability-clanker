# The runner supplies the cluster credential in KUBE_CONFIG_PATH. The access broker issues it.
provider "helm" {}

data "terraform_remote_state" "network" {
  backend = "local"
  config  = { path = "${var.state_location}/10-network.tfstate" }
}

data "terraform_remote_state" "platform" {
  backend = "local"
  config  = { path = "${var.state_location}/40-platform.tfstate" }
}

locals {
  platform = data.terraform_remote_state.platform.outputs
}

module "app" {
  source = "../../../modules/shared/app"

  paperboat_version   = var.paperboat_version
  api_replicas        = var.api_replicas
  worker_replicas     = var.worker_replicas
  web_replicas        = var.web_replicas
  loadgen_enabled     = var.loadgen_enabled
  workload_runtime    = var.workload_runtime
  image_registry      = data.terraform_remote_state.network.outputs.image_registry
  app_namespace       = local.platform.app_namespace
  app_secret_name     = local.platform.app_secret_name
  shared_volume_claim = local.platform.shared_volume_claim
  gateway_name        = local.platform.gateway_name
  gateway_namespace   = local.platform.gateway_namespace
  public_address      = local.platform.public_address
  admin_token         = var.admin_token
  chart_path          = "${path.root}/../../../../apps/paperboat/chart"
}
