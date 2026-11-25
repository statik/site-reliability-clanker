# Layer 10-network of the stand-in stacks of scripts/verify/aws-core-check.sh. Like the real
# layer in its existing-network mode, it has no resource: its outputs pass inputs of the
# account on to the layers above, so its first plan changes only outputs. The state is in
# the MinIO container of the check, which has the name minio on the network of the tasks.
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

variable "state_location" {
  type = string
}

variable "region" {
  type = string
}

variable "existing_network_id" {
  type = string
}

variable "existing_private_subnet_ids" {
  type = list(string)
}

variable "image_registry" {
  type = string
}

output "network_id" {
  value = var.existing_network_id
}

output "private_subnet_ids" {
  value = var.existing_private_subnet_ids
}

output "image_registry" {
  value = var.image_registry
}

output "state_location" {
  value = var.state_location
}
