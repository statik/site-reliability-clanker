locals {
  major_version  = split(".", var.postgres_version)[0]
  admin_username = "postgres"
}

# Cloud SQL does not release an instance name for about a week after deletion.
resource "random_id" "instance_suffix" {
  byte_length = 3
}

resource "google_sql_database_instance" "this" {
  name             = "${var.name}-${random_id.instance_suffix.hex}"
  region           = var.region
  database_version = "POSTGRES_${local.major_version}"

  deletion_protection = var.deletion_protection

  settings {
    tier              = "db-f1-micro"
    edition           = "ENTERPRISE"
    availability_type = "ZONAL"
    disk_type         = "PD_SSD"
    disk_size         = var.storage_gb
    disk_autoresize   = false
    user_labels       = var.labels

    ip_configuration {
      ipv4_enabled    = false
      private_network = var.network_id
    }

    backup_configuration {
      enabled    = true
      start_time = "03:00"

      backup_retention_settings {
        retained_backups = var.backup_retention_days
        retention_unit   = "COUNT"
      }
    }
  }
}

resource "google_sql_database" "app" {
  name     = var.database_name
  instance = google_sql_database_instance.this.name
}

resource "random_password" "admin" {
  length  = 32
  special = false
}

resource "google_sql_user" "admin" {
  name     = local.admin_username
  instance = google_sql_database_instance.this.name
  password = random_password.admin.result
}

resource "google_secret_manager_secret" "admin_password" {
  secret_id = "${var.name}-admin-password"

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "admin_password" {
  secret      = google_secret_manager_secret.admin_password.id
  secret_data = random_password.admin.result

  # The secret must not exist before the database accepts the password.
  depends_on = [google_sql_user.admin]
}
