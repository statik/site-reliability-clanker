# Layer 50-app of the stand-in stacks of scripts/verify/aws-core-check.sh. It has no output:
# with no app_url the verification after the apply has no application to check.
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

resource "terraform_data" "layer" {
  input = "${var.env_name}-app"
}
