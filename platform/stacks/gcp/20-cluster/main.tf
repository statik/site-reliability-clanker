data "terraform_remote_state" "bootstrap" {
  backend = "gcs"

  config = {
    bucket = var.state_location
    prefix = "${var.env_name}/00-bootstrap"
  }
}

data "terraform_remote_state" "network" {
  backend = "gcs"

  config = {
    bucket = var.state_location
    prefix = "${var.env_name}/10-network"
  }
}

locals {
  labels = {
    clanker_env   = var.env_name
    clanker_layer = "20-cluster"
    clanker_ttl   = data.terraform_remote_state.bootstrap.outputs.clanker_ttl
  }
}

provider "google" {
  project = var.project_id
  region  = var.region

  default_labels = local.labels
}

module "kubernetes" {
  source = "../../../modules/gcp/kubernetes"

  name       = var.env_name
  project_id = var.project_id
  region     = var.region
  zones      = data.terraform_remote_state.network.outputs.zones

  network_id          = data.terraform_remote_state.network.outputs.network_id
  subnet_id           = data.terraform_remote_state.network.outputs.private_subnet_ids[0]
  pods_range_name     = data.terraform_remote_state.network.outputs.pods_range_name
  services_range_name = data.terraform_remote_state.network.outputs.services_range_name

  kubernetes_version   = var.kubernetes_version
  node_image_version   = var.node_image_version
  node_count           = var.node_count
  node_size            = var.node_size
  node_service_account = data.terraform_remote_state.bootstrap.outputs.node_service_account

  labels              = local.labels
  deletion_protection = var.deletion_protection
}
