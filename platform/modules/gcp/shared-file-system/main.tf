locals {
  share_name = "shared"
}

resource "google_filestore_instance" "this" {
  name     = var.name
  location = var.zone
  tier     = "BASIC_HDD"

  file_shares {
    name        = local.share_name
    capacity_gb = var.capacity_gb
  }

  networks {
    network           = var.network_id
    modes             = ["MODE_IPV4"]
    connect_mode      = "PRIVATE_SERVICE_ACCESS"
    reserved_ip_range = var.private_services_range_name
  }
}

# A class with no provisioner. A claim binds to the volume below and cannot make a new
# Filestore instance.
resource "kubernetes_storage_class_v1" "rwx" {
  metadata {
    name = var.storage_class_name
  }

  storage_provisioner = "kubernetes.io/no-provisioner"
  reclaim_policy      = "Retain"
  volume_binding_mode = "Immediate"
}

resource "kubernetes_persistent_volume_v1" "shared" {
  metadata {
    name = var.name
  }

  spec {
    capacity = {
      storage = "${var.capacity_gb}Gi"
    }

    access_modes                     = ["ReadWriteMany"]
    persistent_volume_reclaim_policy = "Retain"
    storage_class_name               = kubernetes_storage_class_v1.rwx.metadata[0].name
    volume_mode                      = "Filesystem"

    persistent_volume_source {
      csi {
        driver = "filestore.csi.storage.gke.io"
        volume_handle = join("/", [
          "modeInstance",
          google_filestore_instance.this.location,
          google_filestore_instance.this.name,
          local.share_name,
        ])

        volume_attributes = {
          ip     = google_filestore_instance.this.networks[0].ip_addresses[0]
          volume = local.share_name
        }
      }
    }
  }
}
