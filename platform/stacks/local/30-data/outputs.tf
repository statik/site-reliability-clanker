output "postgres_host" {
  value       = "${kubernetes_service_v1.postgres.metadata[0].name}.${local.namespace}.svc.cluster.local"
  description = "In-cluster DNS name of Postgres."
}

output "postgres_port" {
  value       = 5432
  description = "Postgres port."
}

output "postgres_database" {
  value       = "paperboat"
  description = "Database name for Paperboat."
}

output "postgres_admin_secret_ref" {
  value       = "${local.namespace}/${kubernetes_secret_v1.postgres_admin.metadata[0].name}"
  description = "Namespace and name of the secret with the administrator credential."
}

output "postgres_version" {
  value       = var.postgres_version
  description = "Postgres version that runs now."
}

output "rwx_storage_class" {
  value       = kubernetes_storage_class_v1.shared_rwx.metadata[0].name
  description = "StorageClass that supports ReadWriteMany."
}

output "backup_bucket" {
  value       = local.backup_path
  description = "Directory on the node for database backups."
}

output "data_namespace" {
  value       = local.namespace
  description = "Namespace of the data services in the cluster."
}
