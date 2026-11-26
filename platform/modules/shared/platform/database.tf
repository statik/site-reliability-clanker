# The database of the application and its own role, which is not a superuser. Only the
# cluster reaches Postgres on every target, so a Job in the cluster makes them with psql, as
# the administrator. The Job and the administrator credential are in a namespace of the
# platform. The application namespace gets only the Secret of the role.
locals {
  platform_namespace = "clanker-platform"
  database_admin     = "postgres"
  database_job_name  = "${var.app_name}-database"
  database_sql       = file("${path.module}/database.sql")

  # psql 15 or later, for \getenv. Only the client: the server can have another version.
  # 17.11-alpine
  psql_image = "postgres@sha256:e5d2edd28b1f8d40a0a6c0c9cdc545b19b56a0d5d88f94bc11a97d970d4700d0"

  database_url = format(
    "postgresql://%s:%s@%s:%d/%s?sslmode=%s",
    var.app_name, random_password.database.result, var.postgres_host, var.postgres_port,
    var.app_name, var.postgres_sslmode,
  )

  # The name of the Job ends with a hash of these inputs, so that a change of one of them
  # replaces the Job, and the new Job runs. OpenTofu would update the Job in place for a
  # change of its template, and a Job that exists does not run again. The administrator
  # password is not an input: when it rotates, the role and the database are still there.
  database_job_inputs = nonsensitive(sha256(jsonencode({
    sql      = local.database_sql
    image    = local.psql_image
    host     = var.postgres_host
    port     = var.postgres_port
    sslmode  = var.postgres_sslmode
    role     = var.app_name
    password = random_password.database.result
  })))
}

# No special characters, so that the password needs no escaping in DATABASE_URL.
resource "random_password" "database" {
  length  = 32
  special = false
}

resource "kubernetes_namespace_v1" "platform" {
  metadata {
    name   = local.platform_namespace
    labels = local.labels
  }
}

resource "kubernetes_secret_v1" "database_job" {
  metadata {
    name      = local.database_job_name
    namespace = kubernetes_namespace_v1.platform.metadata[0].name
    labels    = local.labels
  }

  data = {
    PGHOST       = var.postgres_host
    PGPORT       = tostring(var.postgres_port)
    PGDATABASE   = "postgres"
    PGUSER       = local.database_admin
    PGPASSWORD   = var.postgres_admin_password
    PGSSLMODE    = var.postgres_sslmode
    APP_ROLE     = var.app_name
    APP_PASSWORD = random_password.database.result
  }
}

resource "kubernetes_config_map_v1" "database_job" {
  metadata {
    name      = local.database_job_name
    namespace = kubernetes_namespace_v1.platform.metadata[0].name
    labels    = local.labels
  }

  data = {
    "database.sql" = local.database_sql
  }
}

resource "kubernetes_job_v1" "database" {
  metadata {
    name      = "${local.database_job_name}-${substr(local.database_job_inputs, 0, 10)}"
    namespace = kubernetes_namespace_v1.platform.metadata[0].name
    labels    = local.labels
  }

  spec {
    backoff_limit = 4

    template {
      metadata {
        labels = { app = local.database_job_name }
      }

      spec {
        restart_policy = "Never"

        container {
          name    = "psql"
          image   = local.psql_image
          command = ["psql", "--no-psqlrc", "--file=/sql/database.sql"]

          env_from {
            secret_ref {
              name = kubernetes_secret_v1.database_job.metadata[0].name
            }
          }

          resources {
            requests = { cpu = "10m", memory = "32Mi" }
            limits   = { memory = "128Mi" }
          }

          # User postgres of the image.
          security_context {
            run_as_non_root            = true
            run_as_user                = 70
            allow_privilege_escalation = false
            read_only_root_filesystem  = true
            capabilities {
              drop = ["ALL"]
            }
          }

          volume_mount {
            name       = "sql"
            mount_path = "/sql"
            read_only  = true
          }
        }

        volume {
          name = "sql"
          config_map {
            name = kubernetes_config_map_v1.database_job.metadata[0].name
          }
        }
      }
    }
  }

  wait_for_completion = true

  timeouts {
    create = "5m"
    update = "5m"
  }
}

resource "kubernetes_secret_v1" "database" {
  metadata {
    name      = "${var.app_name}-db"
    namespace = kubernetes_namespace_v1.app.metadata[0].name
    labels    = local.labels
  }

  data = {
    PGHOST       = var.postgres_host
    PGPORT       = tostring(var.postgres_port)
    PGDATABASE   = var.app_name
    PGUSER       = var.app_name
    PGPASSWORD   = random_password.database.result
    PGSSLMODE    = var.postgres_sslmode
    DATABASE_URL = local.database_url
  }

  # The role exists before the application can read its credential.
  depends_on = [kubernetes_job_v1.database]
}
