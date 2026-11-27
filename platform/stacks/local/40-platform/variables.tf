variable "env_name" {
  type        = string
  description = "Environment name."
}

variable "state_location" {
  type        = string
  description = "Absolute path of the directory that holds the OpenTofu state files."
}

variable "app_name" {
  type        = string
  description = "Name of the application pack. The namespace, the database role, the database, the Secret <name>-db and the claim <name>-shared have this name."
}

variable "workloads_namespace" {
  type        = bool
  description = "Make the namespace <name>-workloads for the pods that the application starts. The control plane writes this line from deploy.workloads_namespace of the pack."
  default     = false
}

variable "public_port" {
  type        = number
  description = "Host port that serves Paperboat."
  default     = 8088
}

variable "shared_fs_claim_gb" {
  type        = number
  description = "Size of the shared volume claim in GiB."
  default     = 1
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
