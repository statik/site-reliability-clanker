data "terraform_remote_state" "bootstrap" {
  backend = "gcs"

  config = {
    bucket = var.state_location
    prefix = "${var.env_name}/00-bootstrap"
  }
}

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

locals {
  labels = {
    clanker_env   = var.env_name
    clanker_layer = "30-data"
    clanker_ttl   = data.terraform_remote_state.bootstrap.outputs.clanker_ttl
  }
}

provider "google" {
  project = var.project_id
  region  = var.region

  default_labels = local.labels
}

data "google_client_config" "current" {}

provider "kubernetes" {
  host                   = data.terraform_remote_state.cluster.outputs.cluster_endpoint
  token                  = data.google_client_config.current.access_token
  cluster_ca_certificate = data.terraform_remote_state.cluster.outputs.cluster_ca_certificate
}

module "postgres" {
  source = "../../../modules/gcp/postgres"

  name       = "${var.env_name}-postgres"
  region     = var.region
  network_id = data.terraform_remote_state.network.outputs.network_id

  postgres_version      = var.postgres_version
  storage_gb            = var.postgres_storage_gb
  backup_retention_days = var.backup_retention_days
  database_name         = "paperboat"

  labels              = local.labels
  deletion_protection = var.deletion_protection
}

module "shared_file_system" {
  source = "../../../modules/gcp/shared-file-system"

  name                        = "${var.env_name}-shared"
  zone                        = data.terraform_remote_state.network.outputs.zones[0]
  network_id                  = data.terraform_remote_state.network.outputs.network_id
  private_services_range_name = data.terraform_remote_state.network.outputs.private_services_range_name
  capacity_gb                 = var.shared_fs_capacity_gb
  storage_class_name          = "clanker-rwx"
}

module "backup_bucket" {
  source = "../../../modules/gcp/object-storage"

  name          = "${var.project_id}-${var.env_name}-backup"
  location      = var.region
  force_destroy = !var.deletion_protection
}
