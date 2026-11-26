data "terraform_remote_state" "network" {
  backend = "gcs"

  config = {
    bucket = var.state_location
    prefix = "${var.env_name}/10-network"
  }
}

data "terraform_remote_state" "cluster" {
  backend = "gcs"

  config = {
    bucket = var.state_location
    prefix = "${var.env_name}/20-cluster"
  }
}

data "terraform_remote_state" "platform" {
  backend = "gcs"

  config = {
    bucket = var.state_location
    prefix = "${var.env_name}/40-platform"
  }
}

# This layer makes no Google Cloud resource. The provider supplies the cluster token.
provider "google" {
  project = var.project_id
  region  = var.region
}

data "google_client_config" "current" {}

provider "kubernetes" {
  host                   = data.terraform_remote_state.cluster.outputs.cluster_endpoint
  token                  = data.google_client_config.current.access_token
  cluster_ca_certificate = data.terraform_remote_state.cluster.outputs.cluster_ca_certificate
}

provider "helm" {
  kubernetes = {
    host                   = data.terraform_remote_state.cluster.outputs.cluster_endpoint
    token                  = data.google_client_config.current.access_token
    cluster_ca_certificate = data.terraform_remote_state.cluster.outputs.cluster_ca_certificate
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

  image_registry = data.terraform_remote_state.network.outputs.image_registry

  app_namespace       = data.terraform_remote_state.platform.outputs.app_namespace
  app_secret_name     = data.terraform_remote_state.platform.outputs.app_secret_name
  shared_volume_claim = data.terraform_remote_state.platform.outputs.shared_volume_claim
  gateway_name        = data.terraform_remote_state.platform.outputs.gateway_name
  gateway_namespace   = data.terraform_remote_state.platform.outputs.gateway_namespace
  public_address      = data.terraform_remote_state.platform.outputs.public_address

  chart_path = "${path.root}/../../../../apps/paperboat/chart"
}
