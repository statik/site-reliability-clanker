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

variable "node_port" {
  type        = number
  description = "Node port of the Envoy Service. 0 makes it a LoadBalancer Service."
  default     = 0

  validation {
    condition     = var.node_port == 0 || (var.node_port >= 30000 && var.node_port <= 32767)
    error_message = "Set node_port to 0 or to a port from 30000 to 32767."
  }
}

variable "public_address" {
  type        = string
  description = "Known public address. Empty: read it from the load balancer of the Envoy Service."
  default     = ""
}

variable "service_annotations" {
  type        = map(string)
  description = "Annotations for the load balancer Service of Envoy Gateway."
  default     = {}
}

variable "collector_endpoint" {
  type        = string
  description = "Address of the control plane for the data of the collector, for example http://host:7070/otlp. Empty: no collector."
  default     = ""
}

variable "collector_token" {
  type        = string
  sensitive   = true
  description = "Token that the collector sends to the control plane."
  default     = ""
}
