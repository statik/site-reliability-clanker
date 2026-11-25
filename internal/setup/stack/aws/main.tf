provider "aws" {
  region = var.region

  default_tags {
    tags = local.tags
  }
}

data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

# The role of the person who applies this stack, without the session. The key policy of the
# settings names it, because the provider reads each parameter back with decryption.
data "aws_iam_session_context" "operator" {
  arn = data.aws_caller_identity.current.arn
}

data "aws_vpc" "this" {
  id = var.vpc_id
}

data "aws_route53_zone" "this" {
  zone_id = var.zone_id
}

locals {
  name = "clanker-${var.env_name}"
  tags = merge(var.required_tags, { clanker_env = var.env_name })

  account_id = data.aws_caller_identity.current.account_id
  partition  = data.aws_partition.current.partition
  arn_suffix = "${var.region}:${local.account_id}"

  zone_name = trimsuffix(data.aws_route53_zone.this.name, ".")
  core_host = "src-${var.env_name}.${local.zone_name}"
  idp_host  = "src-${var.env_name}-idp.${local.zone_name}"

  # No persisted value names an instance address: people, Keycloak and the collector reach
  # the core by its name on the load balancer.
  core_url           = "https://${local.core_host}"
  idp_url            = "https://${local.idp_host}"
  collector_endpoint = "${local.core_url}/otlp"

  core_port     = 7070
  keycloak_port = 8080

  phase_two = var.core_image_id != null
}
