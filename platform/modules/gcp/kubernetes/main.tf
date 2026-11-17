locals {
  zonal    = length(var.zones) == 1
  location = local.zonal ? var.zones[0] : var.region

  nodes_per_zone = ceil(var.node_count / length(var.zones))
}

resource "google_container_cluster" "this" {
  name           = var.name
  location       = local.location
  node_locations = local.zonal ? null : var.zones

  network         = var.network_id
  subnetwork      = var.subnet_id
  networking_mode = "VPC_NATIVE"

  # GKE resolves a minor version to its default patch release.
  min_master_version = var.kubernetes_version

  # The agent controls each node version change, so the cluster is not in a release channel.
  release_channel {
    channel = "UNSPECIFIED"
  }

  # The cluster has no default pool. GKE 1.36.4-gke.1469000 and later permit that.
  deletion_protection = var.deletion_protection

  ip_allocation_policy {
    cluster_secondary_range_name  = var.pods_range_name
    services_secondary_range_name = var.services_range_name
  }

  private_cluster_config {
    enable_private_nodes    = true
    enable_private_endpoint = false
  }

  workload_identity_config {
    workload_pool = "${var.project_id}.svc.id.goog"
  }

  addons_config {
    gcp_filestore_csi_driver_config {
      enabled = true
    }
  }
}

resource "google_container_node_pool" "default" {
  name     = "default"
  cluster  = google_container_cluster.this.name
  location = google_container_cluster.this.location

  node_count = local.nodes_per_zone
  version = coalesce(
    var.node_image_version,
    google_container_cluster.this.master_version,
  )

  management {
    auto_repair  = true
    auto_upgrade = false
  }

  upgrade_settings {
    max_surge       = 1
    max_unavailable = 0
  }

  node_config {
    machine_type    = var.node_size
    image_type      = "COS_CONTAINERD"
    disk_type       = "pd-balanced"
    disk_size_gb    = 30
    service_account = var.node_service_account
    oauth_scopes    = ["https://www.googleapis.com/auth/cloud-platform"]
    resource_labels = var.labels

    workload_metadata_config {
      mode = "GKE_METADATA"
    }

    shielded_instance_config {
      enable_secure_boot          = true
      enable_integrity_monitoring = true
    }
  }
}
