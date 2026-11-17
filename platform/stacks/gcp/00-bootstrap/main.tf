locals {
  # A label value cannot contain a timestamp, so the label holds the lifetime.
  clanker_ttl = var.ttl_hours == 0 ? "none" : "${var.ttl_hours}h"

  required_services = [
    "artifactregistry.googleapis.com",
    "cloudresourcemanager.googleapis.com",
    "compute.googleapis.com",
    "container.googleapis.com",
    "file.googleapis.com",
    "iam.googleapis.com",
    "iamcredentials.googleapis.com",
    "logging.googleapis.com",
    "monitoring.googleapis.com",
    "secretmanager.googleapis.com",
    "servicenetworking.googleapis.com",
    "sqladmin.googleapis.com",
    "storage.googleapis.com",
  ]
}

provider "google" {
  project = var.project_id
  region  = var.region

  default_labels = {
    clanker_env   = var.env_name
    clanker_layer = "00-bootstrap"
    clanker_ttl   = local.clanker_ttl
  }
}

resource "google_project_service" "required" {
  for_each = toset(local.required_services)

  project            = var.project_id
  service            = each.key
  disable_on_destroy = false
}

module "state_bucket" {
  source = "../../../modules/gcp/object-storage"

  name     = var.state_location
  location = var.region

  depends_on = [google_project_service.required]
}

module "evidence_bucket" {
  source = "../../../modules/gcp/object-storage"

  name     = "${var.project_id}-${var.env_name}-evidence"
  location = var.region

  depends_on = [google_project_service.required]
}

module "agent_identities" {
  source = "../../../modules/gcp/agent-identities"

  project_id       = var.project_id
  state_bucket     = module.state_bucket.name
  broker_principal = var.broker_principal
  grant_windows    = var.grant_windows

  depends_on = [google_project_service.required]
}
