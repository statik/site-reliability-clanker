# Layer 00-bootstrap of the check: the inputs and the outputs of the real layer, and one
# sensitive output that the setting CLANKER_AWS_BOOTSTRAP must leave out. The check makes
# the bucket itself; module "state_bucket" stands for it, as in the real layer.
terraform {
  backend "s3" {
    use_path_style              = true
    skip_credentials_validation = true
    skip_requesting_account_id  = true
    skip_region_validation      = true
    skip_metadata_api_check     = true
  }
}

variable "env_name" {
  type = string
}

variable "state_location" {
  type = string
}

variable "region" {
  type = string
}

# Required and checked, as in the real layer, so a destroy must give it too.
variable "broker_principal" {
  type = string

  validation {
    condition     = can(regex("^arn:aws:iam::[0-9]{12}:role/.+$", var.broker_principal))
    error_message = "broker_principal must be an IAM role ARN."
  }
}

variable "core_parameter_path" {
  type    = string
  default = "/src"
}

# From terraform.tfvars.
variable "ttl_hours" {
  type = number
}

module "state_bucket" {
  source = "./bucket"

  name = var.state_location
}

resource "terraform_data" "identities" {
  input = {
    broker_principal    = var.broker_principal
    core_parameter_path = var.core_parameter_path
  }
}

output "state_backend" {
  value = {
    bucket       = module.state_bucket.name
    region       = var.region
    encrypt      = "true"
    use_lockfile = "true"
  }
}

output "evidence_bucket" {
  value = "clanker-${var.env_name}-evidence-000000000000"
}

output "identities" {
  value = {
    observer = "arn:aws:iam::000000000000:role/clanker/${var.env_name}/observer"
    planner  = "arn:aws:iam::000000000000:role/clanker/${var.env_name}/planner"
  }
}

output "permission_guard" {
  value = "arn:aws:iam::000000000000:policy/clanker/${var.env_name}-boundary"
}

output "clanker_ttl" {
  value = tostring(var.ttl_hours)
}

output "workload_role_path" {
  value = "/clanker/${var.env_name}/workload/"
}

output "fixture_secret" {
  value     = "fixture-secret-value"
  sensitive = true
}
