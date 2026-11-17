output "postgres_host" {
  description = "Private IP address of the database."
  value       = module.postgres.host
}

output "postgres_port" {
  description = "PostgreSQL port."
  value       = module.postgres.port
}

output "postgres_database" {
  description = "Database name for Paperboat."
  value       = module.postgres.database
}

output "postgres_admin_secret_ref" {
  description = "Secret Manager secret that holds the password of the postgres user."
  value       = module.postgres.admin_secret_ref
}

output "postgres_version" {
  description = "PostgreSQL version that runs now."
  value       = module.postgres.postgres_version
}

output "rwx_storage_class" {
  description = "Name of a StorageClass that supports ReadWriteMany."
  value       = module.shared_file_system.rwx_storage_class
}

output "backup_bucket" {
  description = "Bucket for database backups."
  value       = module.backup_bucket.name
}

output "shared_fs_capacity_gb" {
  description = "Capacity of the shared file system in GiB. Layer 40-platform sizes its claim from it."
  value       = var.shared_fs_capacity_gb
}
