output "host" {
  description = "Private IP address of the instance."
  value       = google_sql_database_instance.this.private_ip_address
}

output "port" {
  description = "PostgreSQL port."
  value       = 5432
}

output "admin_secret_ref" {
  description = "Secret Manager secret that holds the password of the postgres user."
  value       = google_secret_manager_secret.admin_password.id

  # A reader of this output needs a secret version.
  depends_on = [google_secret_manager_secret_version.admin_password]
}

output "postgres_version" {
  description = "Version that runs now. The major version only, if Cloud SQL does not report the minor."
  value = try(
    join(".", regex(
      "^POSTGRES_([0-9]+)_([0-9]+)", google_sql_database_instance.this.maintenance_version
    )),
    trimprefix(google_sql_database_instance.this.database_version, "POSTGRES_"),
  )
}
