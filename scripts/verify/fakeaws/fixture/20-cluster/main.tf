# Layer 20-cluster of the stand-in stacks of scripts/verify/aws-core-check.sh. It reads the
# outputs of 10-network from its state in the bucket, as the real layer does, and takes a
# map and a string of the inputs of the account. Its cluster is the stand-in API server of
# fakeaws: the check gives its address and its certificate.
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

variable "instance_tags" {
  type = map(string)
}

variable "operator_principal_arn" {
  type = string
}

variable "fake_cluster_endpoint" {
  type = string
}

variable "fake_cluster_ca_data" {
  type = string
}

data "terraform_remote_state" "network" {
  backend = "s3"
  config = {
    bucket                      = var.state_location
    key                         = "${var.env_name}/10-network.tfstate"
    region                      = var.region
    endpoints                   = { s3 = "http://minio:9000" }
    use_path_style              = true
    skip_credentials_validation = true
    skip_requesting_account_id  = true
    skip_region_validation      = true
    skip_metadata_api_check     = true
  }
}

resource "terraform_data" "cluster" {
  input = {
    network_id    = data.terraform_remote_state.network.outputs.network_id
    tag_keys      = sort(keys(var.instance_tags))
    operator_role = var.operator_principal_arn
  }
}

output "cluster_name" {
  value = "${var.env_name}-cluster"
}

output "cluster_endpoint" {
  value = var.fake_cluster_endpoint
}

output "cluster_certificate_authority_data" {
  value = var.fake_cluster_ca_data
}

output "network_id" {
  value = terraform_data.cluster.output.network_id
}

output "instance_tag_keys" {
  value = terraform_data.cluster.output.tag_keys
}
