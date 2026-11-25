locals {
  layer    = "10-network"
  existing = var.existing_network_id != ""
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
  count  = local.existing ? 0 : 1

  name       = "clanker-${var.env_name}"
  cidr       = var.network_cidr
  zone_count = var.zone_count
}

# In existing-network mode the layer only reads the subnets, so that a wrong ID stops the
# plan of this layer and not the apply of a layer above it.
data "aws_subnet" "existing" {
  for_each = toset(var.existing_private_subnet_ids)

  id = each.value

  lifecycle {
    postcondition {
      condition     = self.vpc_id == var.existing_network_id
      error_message = "A subnet in existing_private_subnet_ids is not in existing_network_id."
    }
  }
}
