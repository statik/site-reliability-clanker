locals {
  network  = data.terraform_remote_state.network.outputs
  cluster  = data.terraform_remote_state.cluster.outputs
  platform = data.terraform_remote_state.platform.outputs

  eks_token_args = [
    "eks", "get-token", "--cluster-name", local.cluster.cluster_name, "--region", var.region,
  ]
}

data "terraform_remote_state" "network" {
  backend = "s3"

  config = {
    bucket = var.state_location
    key    = "${var.env_name}/10-network.tfstate"
    region = var.region
  }
}

data "terraform_remote_state" "cluster" {
  backend = "s3"

  config = {
    bucket = var.state_location
    key    = "${var.env_name}/20-cluster.tfstate"
    region = var.region
  }
}

data "terraform_remote_state" "platform" {
  backend = "s3"

  config = {
    bucket = var.state_location
    key    = "${var.env_name}/40-platform.tfstate"
    region = var.region
  }
}

provider "helm" {
  kubernetes = {
    host                   = local.cluster.cluster_endpoint
    cluster_ca_certificate = local.cluster.cluster_ca_certificate

    exec = {
      api_version = "client.authentication.k8s.io/v1beta1"
      command     = "aws"
      args        = local.eks_token_args
    }
  }
}

module "app" {
  source = "../../../modules/shared/app"

  app                 = local.app
  cloud               = "aws"
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
