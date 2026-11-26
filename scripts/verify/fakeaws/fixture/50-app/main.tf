# Layer 50-app of the stand-in stacks of scripts/verify/aws-core-check.sh. It has the static
# shape of the real layer: the seed of the core writes app.tf beside it from the manifest of
# the pack, and app.tf gives local.app to the module and names its outputs. The stand-in
# module returns null for the URL and the version, and OpenTofu keeps no null output: with
# no app_url the verification after the apply has no application to check.
terraform {
  backend "s3" {
    endpoints                   = { s3 = "http://minio:9000" }
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

module "app" {
  source = "./standin"

  app      = local.app
  env_name = var.env_name
}
