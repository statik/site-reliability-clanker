variable "name" {
  description = "Instance identifier and name prefix of the related resources."
  type        = string
}

variable "engine_version" {
  description = "Full PostgreSQL version, for example 17.10."
  type        = string
}

variable "storage_gb" {
  description = "Allocated storage in GiB. The gp3 minimum is 20."
  type        = number

  validation {
    condition     = var.storage_gb >= 20
    error_message = "storage_gb must be 20 or more. That is the minimum for gp3 storage on RDS."
  }
}

variable "backup_retention_days" {
  description = "Retention for automatic backups, in days."
  type        = number

  validation {
    condition     = var.backup_retention_days >= 1 && var.backup_retention_days <= 35
    error_message = "backup_retention_days must be from 1 to 35. 0 would disable backups."
  }
}

variable "admin_username" {
  description = "Name of the master user."
  type        = string
}

variable "vpc_id" {
  description = "VPC of the instance."
  type        = string
}

variable "subnet_ids" {
  description = "Private subnets for the instance, in two zones or more."
  type        = list(string)
}

variable "client_security_group_id" {
  description = "Security group that can connect to the database."
  type        = string
}
