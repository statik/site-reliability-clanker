# Layer 40-platform of the stand-in stacks of scripts/verify/aws-core-check.sh. It takes the
# two variables of the collector that the core gives the jobs, and reports the endpoint and
# the hash of the token, so that the check can compare them with the core. Its targets say
# where a debug session reads, as the targets of the real layer do.
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

variable "collector_endpoint" {
  type = string
}

variable "collector_token" {
  type      = string
  sensitive = true
}

resource "terraform_data" "layer" {
  input = "${var.env_name}-platform"
}

output "name" {
  value = terraform_data.layer.output
}

output "collector_endpoint" {
  value = var.collector_endpoint
}

output "collector_token_sha256" {
  value = nonsensitive(sha256(var.collector_token))
}

output "targets" {
  value = {
    application = "paperboat"
    data        = ""
  }
}
