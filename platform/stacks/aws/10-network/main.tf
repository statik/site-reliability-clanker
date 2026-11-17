locals {
  layer = "10-network"
}

data "terraform_remote_state" "bootstrap" {
  backend = "s3"

  config = {
    bucket = var.state_location
    key    = "${var.env_name}/00-bootstrap.tfstate"
    region = var.region
  }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      clanker_env   = var.env_name
      clanker_layer = local.layer
      clanker_ttl   = data.terraform_remote_state.bootstrap.outputs.clanker_ttl
    }
  }
}

module "network" {
  source = "../../../modules/aws/network"

  name       = "clanker-${var.env_name}"
  cidr       = var.network_cidr
  zone_count = var.zone_count
}

module "registry" {
  source = "../../../modules/aws/registry"

  repository_name = "paperboat"
}
