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

provider "kubernetes" {
  host                   = local.cluster.cluster_endpoint
  cluster_ca_certificate = local.cluster.cluster_ca_certificate

  exec {
    api_version = "client.authentication.k8s.io/v1beta1"
    command     = "aws"
    args        = local.eks_token_args
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

  paperboat_version = var.paperboat_version
  api_replicas      = var.api_replicas
  worker_replicas   = var.worker_replicas
  web_replicas      = var.web_replicas
  loadgen_enabled   = var.loadgen_enabled
  workload_runtime  = var.workload_runtime

  image_registry      = local.network.image_registry
  app_namespace       = local.platform.app_namespace
  app_secret_name     = local.platform.app_secret_name
  shared_volume_claim = local.platform.shared_volume_claim
  gateway_class       = local.platform.gateway_class
  public_address      = local.platform.public_address
  chart_path          = "${path.root}/../../../../apps/paperboat/chart"
}
