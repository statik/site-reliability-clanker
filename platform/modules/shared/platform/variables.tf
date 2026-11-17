variable "env_name" {
  type        = string
  description = "Environment name."
}

variable "app_namespace" {
  type        = string
  description = "Namespace for Paperboat."
  default     = "paperboat"
}

variable "public_port" {
  type        = number
  description = "Port of the public listener."
  default     = 80
}

variable "postgres_host" {
  type        = string
  description = "Private host name of Postgres."
}

variable "postgres_port" {
  type        = number
  description = "Postgres port."
}

variable "postgres_database" {
  type        = string
  description = "Database name for Paperboat."
}

variable "postgres_admin_password" {
  type        = string
  sensitive   = true
  description = "Password of the postgres user."
}

variable "rwx_storage_class" {
  type        = string
  description = "StorageClass that supports ReadWriteMany."
}

variable "shared_fs_capacity_gb" {
  type        = number
  description = "Size of the shared volume claim in GiB."
}

variable "gateway_class" {
  type        = string
  description = "nodeport for targets without the Gateway API, or envoy to install Envoy Gateway."

  validation {
    condition     = contains(["nodeport", "envoy"], var.gateway_class)
    error_message = "Set gateway_class to nodeport or envoy."
  }
}

variable "public_address" {
  type        = string
  description = "Known public address. Empty means: read it from the Gateway status."
  default     = ""
}

variable "service_annotations" {
  type        = map(string)
  description = "Annotations for the load balancer Service of Envoy Gateway."
  default     = {}
}
