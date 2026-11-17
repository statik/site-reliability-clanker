variable "env_name" {
  type        = string
  description = "Environment name."
}

variable "state_location" {
  type        = string
  description = "Absolute path of the directory that holds the OpenTofu state files."
}

variable "app_namespace" {
  type        = string
  description = "Namespace for Paperboat."
  default     = "paperboat"
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
