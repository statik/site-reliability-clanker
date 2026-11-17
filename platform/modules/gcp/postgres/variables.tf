variable "name" {
  description = "Name prefix for the instance and the secret."
  type        = string
}

variable "region" {
  description = "Region of the instance."
  type        = string
}

variable "network_id" {
  description = "ID of the VPC network. It must have a private services access connection."
  type        = string
}

variable "postgres_version" {
  description = "PostgreSQL version, for example 17.5. Cloud SQL uses only the major version."
  type        = string

  validation {
    condition     = can(regex("^[0-9]+(\\.[0-9]+)?$", var.postgres_version))
    error_message = "postgres_version must be a version such as 17 or 17.5."
  }
}

variable "storage_gb" {
  description = "Database storage in GB."
  type        = number

  validation {
    condition     = var.storage_gb >= 10
    error_message = "storage_gb must be 10 or more. Cloud SQL has a 10 GB minimum."
  }
}

variable "backup_retention_days" {
  description = "Number of daily automatic backups to keep."
  type        = number

  validation {
    condition     = var.backup_retention_days >= 1 && var.backup_retention_days <= 365
    error_message = "backup_retention_days must be between 1 and 365."
  }
}

variable "database_name" {
  description = "Name of the application database."
  type        = string
}

variable "labels" {
  description = "Labels for the instance. The provider default labels do not reach it."
  type        = map(string)
}

variable "deletion_protection" {
  description = "Refuse to destroy the instance."
  type        = bool
}
