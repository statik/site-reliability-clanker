locals {
  layer       = "00-bootstrap"
  clanker_ttl = tostring(var.ttl_hours)

  applier_layers = ["10-network", "20-cluster", "30-data", "40-platform", "50-app"]
}

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      clanker_env   = var.env_name
      clanker_layer = local.layer
      clanker_ttl   = local.clanker_ttl
    }
  }
}

data "aws_caller_identity" "current" {}

module "state_bucket" {
  source = "../../../modules/aws/bucket"

  name = var.state_location
}

module "evidence_bucket" {
  source = "../../../modules/aws/bucket"

  name = "clanker-${var.env_name}-evidence-${data.aws_caller_identity.current.account_id}"
}

module "agent_identities" {
  source = "../../../modules/aws/agent-identities"

  env_name            = var.env_name
  broker_principal    = var.broker_principal
  applier_layers      = local.applier_layers
  state_bucket_arn    = module.state_bucket.arn
  evidence_bucket_arn = module.evidence_bucket.arn
}
