data "terraform_remote_state" "network" {
  backend = "gcs"

  config = {
    bucket = var.state_location
    prefix = "${var.env_name}/10-network"
  }
}

data "terraform_remote_state" "cluster" {
  backend = "gcs"

  config = {
    bucket = var.state_location
    prefix = "${var.env_name}/20-cluster"
  }
}

data "terraform_remote_state" "data" {
  backend = "gcs"

  config = {
    bucket = var.state_location
    prefix = "${var.env_name}/30-data"
  }
}

# This layer makes no Google Cloud resource. The provider supplies the cluster token and
# reads the database password.
provider "google" {
  project = var.project_id
  region  = var.region
}

data "google_client_config" "current" {}

provider "kubernetes" {
  host                   = data.terraform_remote_state.cluster.outputs.cluster_endpoint
  token                  = data.google_client_config.current.access_token
  cluster_ca_certificate = data.terraform_remote_state.cluster.outputs.cluster_ca_certificate
}

provider "helm" {
  kubernetes = {
    host                   = data.terraform_remote_state.cluster.outputs.cluster_endpoint
    token                  = data.google_client_config.current.access_token
    cluster_ca_certificate = data.terraform_remote_state.cluster.outputs.cluster_ca_certificate
  }
}

data "google_secret_manager_secret_version" "postgres_admin" {
  secret = data.terraform_remote_state.data.outputs.postgres_admin_secret_ref
}

module "platform" {
  source = "../../../modules/shared/platform"

  env_name      = var.env_name
  app_namespace = var.app_namespace
  public_port   = var.public_port

  postgres_host           = data.terraform_remote_state.data.outputs.postgres_host
  postgres_port           = data.terraform_remote_state.data.outputs.postgres_port
  postgres_database       = data.terraform_remote_state.data.outputs.postgres_database
  postgres_admin_password = data.google_secret_manager_secret_version.postgres_admin.secret_data

  rwx_storage_class     = data.terraform_remote_state.data.outputs.rwx_storage_class
  shared_fs_capacity_gb = data.terraform_remote_state.data.outputs.shared_fs_capacity_gb

  gateway_class  = "envoy"
  public_address = data.terraform_remote_state.network.outputs.public_address
}
