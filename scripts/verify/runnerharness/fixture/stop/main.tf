# The stack of the stop case of scripts/verify/runner-image-check.sh. Its create runs until
# the check stops the runner container. It uses no cloud provider. Its state is in the MinIO
# container of the check, as the state of the stack in the directory above.
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

resource "terraform_data" "slow" {
  provisioner "local-exec" {
    command = "echo the slow create started; sleep 600"
  }
}
