# Layer 10-network of the check: the inputs that a job gives on AWS and one resource.
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

# Required and checked, as in the real layer. Neither terraform.tfvars nor the command gives
# it: a destroy gets it from TF_VAR_image_registry.
variable "image_registry" {
  type = string

  validation {
    condition     = can(regex("^[a-z0-9.-]+(:[0-9]+)?(/[a-z0-9._-]+)*$", var.image_registry))
    error_message = "image_registry must be a host and optional path."
  }
}

resource "terraform_data" "layer" {
  input = "10-network of ${var.env_name} in ${var.state_location}, ${var.region}"
}
