# Layer 20-cluster of the check: the inputs that a job gives on AWS and one resource.
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

resource "terraform_data" "layer" {
  input = "20-cluster of ${var.env_name} in ${var.state_location}, ${var.region}"
}
