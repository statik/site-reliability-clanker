variable "env_name" {
  type        = string
  description = "Environment name."
}

variable "app_name" {
  type        = string
  description = "Name of the application pack. Its namespace, its database role, its database, the Secret <name>-db and the claim <name>-shared have this name."

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{0,30}$", var.app_name))
    error_message = "Set app_name to the name of the pack: a lowercase letter, then up to 30 lowercase letters, digits or hyphens."
  }
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

variable "postgres_sslmode" {
  type        = string
  description = "libpq sslmode for connections to Postgres: disable, require, verify-ca or verify-full."

  validation {
    condition     = contains(["disable", "require", "verify-ca", "verify-full"], var.postgres_sslmode)
    error_message = "Set postgres_sslmode to disable, require, verify-ca or verify-full. Some drivers of applications refuse other modes."
  }
}

variable "postgres_admin_password" {
  type        = string
  sensitive   = true
  description = "Password of the postgres user, the administrator of the server."
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
