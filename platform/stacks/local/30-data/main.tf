# The runner supplies the cluster credential in KUBE_CONFIG_PATH. The access broker issues it.
provider "kubernetes" {}

locals {
  namespace   = "paperboat-data"
  shared_path = "/var/clanker/shared"
  backup_path = "/var/clanker/backups"
  labels = {
    clanker_env   = var.env_name
    clanker_layer = "30-data"
    clanker_ttl   = "none"
  }
}

resource "kubernetes_namespace_v1" "data" {
  metadata {
    name   = local.namespace
    labels = local.labels
  }
}

resource "random_password" "postgres_admin" {
  length  = 32
  special = false
}

resource "kubernetes_secret_v1" "postgres_admin" {
  metadata {
    name      = "postgres-admin"
    namespace = kubernetes_namespace_v1.data.metadata[0].name
    labels    = local.labels
  }

  data = {
    username = "postgres"
    password = random_password.postgres_admin.result
  }
}

resource "kubernetes_config_map_v1" "retention" {
  metadata {
    name      = "backup-policy"
    namespace = kubernetes_namespace_v1.data.metadata[0].name
    labels    = local.labels
  }

  data = {
    retention_days = tostring(var.backup_retention_days)
    backup_path    = local.backup_path
  }
}

resource "kubernetes_service_v1" "postgres" {
  metadata {
    name      = "postgres"
    namespace = kubernetes_namespace_v1.data.metadata[0].name
    labels    = local.labels
  }

  spec {
    selector = { app = "postgres" }

    port {
      port        = 5432
      target_port = 5432
    }
  }
}

resource "kubernetes_stateful_set_v1" "postgres" {
  metadata {
    name      = "postgres"
    namespace = kubernetes_namespace_v1.data.metadata[0].name
    labels    = local.labels
  }

  spec {
    service_name = kubernetes_service_v1.postgres.metadata[0].name
    replicas     = 1

    selector {
      match_labels = { app = "postgres" }
    }

    template {
      metadata {
        labels = { app = "postgres" }
      }

      spec {
        container {
          name  = "postgres"
          image = "postgres:${var.postgres_version}-alpine"

          port {
            container_port = 5432
          }

          env {
            name  = "POSTGRES_DB"
            value = "paperboat"
          }

          env {
            name  = "PGDATA"
            value = "/var/lib/postgresql/data/pgdata"
          }

          env {
            name = "POSTGRES_PASSWORD"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.postgres_admin.metadata[0].name
                key  = "password"
              }
            }
          }

          volume_mount {
            name       = "data"
            mount_path = "/var/lib/postgresql/data"
          }

          volume_mount {
            name       = "backups"
            mount_path = "/backups"
          }

          readiness_probe {
            exec {
              command = ["pg_isready", "-U", "postgres", "-d", "paperboat"]
            }
            period_seconds = 5
          }
        }

        volume {
          name = "backups"
          host_path {
            path = local.backup_path
            type = "DirectoryOrCreate"
          }
        }
      }
    }

    volume_claim_template {
      metadata {
        name = "data"
      }

      spec {
        access_modes = ["ReadWriteOnce"]

        resources {
          requests = { storage = "${var.postgres_storage_gb}Gi" }
        }
      }
    }
  }
}

resource "kubernetes_storage_class_v1" "shared_rwx" {
  metadata {
    name   = "shared-rwx"
    labels = local.labels
  }

  storage_provisioner = "kubernetes.io/no-provisioner"
  volume_binding_mode = "Immediate"
  reclaim_policy      = "Retain"
}

resource "kubernetes_persistent_volume_v1" "shared" {
  metadata {
    name   = "paperboat-shared"
    labels = local.labels
  }

  spec {
    capacity                         = { storage = "${var.shared_fs_capacity_gb}Gi" }
    access_modes                     = ["ReadWriteMany"]
    storage_class_name               = kubernetes_storage_class_v1.shared_rwx.metadata[0].name
    persistent_volume_reclaim_policy = "Retain"

    persistent_volume_source {
      host_path {
        path = local.shared_path
        type = "DirectoryOrCreate"
      }
    }
  }
}

# A hostPath directory starts with owner root. Paperboat runs as a non-root user.
resource "kubernetes_job_v1" "prepare_shared" {
  metadata {
    name      = "prepare-shared-volume"
    namespace = kubernetes_namespace_v1.data.metadata[0].name
    labels    = local.labels
  }

  spec {
    backoff_limit = 2

    template {
      metadata {
        labels = { app = "prepare-shared-volume" }
      }

      spec {
        restart_policy = "Never"

        container {
          name    = "chmod"
          image   = "busybox:1.38.0"
          command = ["sh", "-c", "chmod 0777 /shared"]

          volume_mount {
            name       = "shared"
            mount_path = "/shared"
          }
        }

        volume {
          name = "shared"
          host_path {
            path = local.shared_path
            type = "DirectoryOrCreate"
          }
        }
      }
    }
  }

  wait_for_completion = true

  timeouts {
    create = "5m"
  }
}
