output "postgres_host" {
  value       = module.postgres.host
  description = "Private host name of the PostgreSQL server."
}

output "postgres_port" {
  value       = 5432
  description = "PostgreSQL port."
}

output "postgres_sslmode" {
  value       = "require"
  description = "libpq sslmode for connections to PostgreSQL. The server takes TLS connections."
}

output "postgres_admin_secret_ref" {
  value       = module.postgres.admin_secret_ref
  description = "Key Vault URL of the administrator password, without a version."
}

output "postgres_version" {
  value       = module.postgres.major_version
  description = "PostgreSQL major version that runs now. Azure selects the minor version."
}

output "rwx_storage_class" {
  value       = module.shared_files.storage_class_name
  description = "StorageClass that supports ReadWriteMany."
}

output "backup_bucket" {
  value       = module.backup_storage.backup_bucket
  description = "Backup location as <storage account>/<container>."
}

output "postgres_admin_user" {
  value       = module.postgres.administrator_login
  description = "Administrator login of the PostgreSQL server."
}

output "postgres_admin_password" {
  value       = module.postgres.admin_password
  description = "Administrator password. Layer 40-platform reads it through remote state."
  sensitive   = true
}

output "shared_fs_capacity_gb" {
  value       = var.shared_fs_capacity_gb
  description = "Provisioned size of the shared file system."
}

output "key_vault_id" {
  value       = module.key_vault.key_vault_id
  description = "Key Vault of the environment."
}
