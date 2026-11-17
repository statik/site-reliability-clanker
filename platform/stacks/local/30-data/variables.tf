variable "env_name" {
  type        = string
  description = "Environment name."
}

variable "state_location" {
  type        = string
  description = "Absolute path of the directory that holds the OpenTofu state files."
}

variable "postgres_version" {
  type        = string
  description = "Full Postgres version. It selects the image tag postgres:<version>-alpine."
}

variable "postgres_storage_gb" {
  type        = number
  description = "Database storage in GiB."
}

variable "shared_fs_capacity_gb" {
  type        = number
  description = "Capacity of the shared volume in GiB."
}

variable "backup_retention_days" {
  type        = number
  description = "Days to keep database backups."
}
