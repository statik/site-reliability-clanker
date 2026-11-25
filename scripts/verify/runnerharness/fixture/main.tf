# The stack of scripts/verify/runner-image-check.sh. It uses no cloud provider. Its state is
# in the MinIO container of the check, which has the name minio on the network of the check.
# The job gives the bucket, the key, the region and the locking through -backend-config.
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

variable "message" {
  type = string
}

# The runner sets it to the bucket of the backend of the job.
variable "state_location" {
  type = string
}

resource "terraform_data" "marker" {
  input = var.message
}

output "message" {
  value = terraform_data.marker.output
}

output "state_location" {
  value = var.state_location
}
