variable "env_name" {
  type        = string
  description = "Environment name, for example azure-sandbox."
}

variable "state_location" {
  type        = string
  description = "State backend as <storage account>/<container>."
}

variable "subscription_id" {
  type        = string
  description = "Azure subscription that holds the environment."
}

variable "location" {
  type        = string
  description = "Azure region."
}

variable "kubelogin_login" {
  type        = string
  description = "kubelogin login mode for the Kubernetes provider, for example azurecli or workloadidentity."
  default     = "azurecli"
}

variable "postgres_version" {
  type        = string
  description = "Full PostgreSQL version, for example 17.5. Azure selects the minor version, so only the major part is used."
}

variable "postgres_storage_gb" {
  type        = number
  description = "Database storage in GiB. The flexible server minimum is 32."
}

variable "shared_fs_capacity_gb" {
  type        = number
  description = "Shared file system capacity in GiB. The premium file share minimum is 100."
}

variable "backup_retention_days" {
  type        = number
  description = "Retention for automatic backups, 7 to 35 days."
}
