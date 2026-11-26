output "postgres_host" {
  description = "Private DNS name of the RDS instance."
  value       = module.postgres.host
}

output "postgres_port" {
  description = "Port."
  value       = module.postgres.port
}

output "postgres_sslmode" {
  description = "libpq sslmode for connections to PostgreSQL. The server takes TLS connections."
  value       = "require"
}

output "postgres_admin_secret_ref" {
  description = "ARN of the Secrets Manager secret. Its JSON has the keys username and password."
  value       = module.postgres.admin_secret_arn
}

output "postgres_version" {
  description = "PostgreSQL version that runs now."
  value       = module.postgres.engine_version
}

output "rwx_storage_class" {
  description = "StorageClass that supports ReadWriteMany. It makes one EFS access point per claim."
  value       = kubernetes_storage_class_v1.rwx.metadata[0].name
}

output "shared_fs_capacity_gb" {
  description = "Size for the ReadWriteMany claim. EFS does not enforce it."
  value       = var.shared_fs_capacity_gb
}

output "backup_bucket" {
  description = "S3 bucket for database backups."
  value       = module.backup_bucket.name
}
