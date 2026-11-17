variable "env_name" {
  description = "Environment name, for example gcp-sandbox."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,19}$", var.env_name))
    error_message = "env_name must be 2 to 20 characters: lowercase letters, digits and hyphens, with a letter first."
  }
}

variable "state_location" {
  description = "Name of the Cloud Storage bucket for OpenTofu state."
  type        = string
}

variable "project_id" {
  description = "Google Cloud project that holds the environment."
  type        = string
}

variable "region" {
  description = "Google Cloud region, for example us-central1."
  type        = string
}

variable "postgres_version" {
  description = "PostgreSQL version, for example 17.5. Cloud SQL uses only the major version."
  type        = string
}

variable "postgres_storage_gb" {
  description = "Database storage in GB. The minimum is 10."
  type        = number
}

variable "shared_fs_capacity_gb" {
  description = "Shared file system capacity in GiB. The minimum is 1024."
  type        = number
}

variable "backup_retention_days" {
  description = "Number of daily automatic database backups to keep."
  type        = number
}

variable "deletion_protection" {
  description = <<-EOT
    Refuse to destroy the database, and keep the backup bucket when it contains objects.
    Set false for an environment that has a TTL.
  EOT
  type        = bool
  default     = true
}
