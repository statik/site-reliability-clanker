data "terraform_remote_state" "bootstrap" {
  backend = "gcs"

  config = {
    bucket = var.state_location
    prefix = "${var.env_name}/00-bootstrap"
  }
}

provider "google" {
  project = var.project_id
  region  = var.region

  default_labels = {
    clanker_env   = var.env_name
    clanker_layer = "10-network"
    clanker_ttl   = data.terraform_remote_state.bootstrap.outputs.clanker_ttl
  }
}

module "network" {
  source = "../../../modules/gcp/network"

  name         = var.env_name
  region       = var.region
  network_cidr = var.network_cidr
  zone_count   = var.zone_count
}

module "image_registry" {
  source = "../../../modules/gcp/image-registry"

  name   = "${var.env_name}-paperboat"
  region = var.region
}
