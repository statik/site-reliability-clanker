variable "env_name" {
  description = "Environment name, for example aws-sandbox."
  type        = string
}

variable "state_location" {
  description = "Name of the S3 bucket that holds OpenTofu state."
  type        = string
}

variable "region" {
  description = "AWS region of the environment."
  type        = string
}

variable "postgres_version" {
  description = "Full PostgreSQL version, for example 17.10."
  type        = string
}

variable "postgres_storage_gb" {
  description = "Database storage in GiB."
  type        = number
}

variable "shared_fs_capacity_gb" {
  description = <<-EOT
    Shared file system capacity in GiB. EFS is elastic and has no size setting. This layer only
    passes the value on in an output, and 40-platform uses it as the size of the claim.
  EOT
  type        = number
}

variable "backup_retention_days" {
  description = "Retention for automatic database backups, in days."
  type        = number
}
