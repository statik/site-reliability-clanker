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

  app                 = local.app
  cloud               = "local"
  pack_dir            = "${path.root}/../../../../apps/${local.app.name}"
  image_registry      = data.terraform_remote_state.network.outputs.image_registry
  app_namespace       = local.platform.app_namespace
  app_secret_name     = local.platform.app_secret_name
  shared_volume_claim = local.platform.shared_volume_claim
  gateway_name        = local.platform.gateway_name
  gateway_namespace   = local.platform.gateway_namespace
  public_address      = local.platform.public_address
  public_port         = local.platform.public_port
  app_domain          = local.platform.app_domain
  admin_token         = var.admin_token
}
