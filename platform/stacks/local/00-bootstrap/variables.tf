variable "env_name" {
  type        = string
  description = "Environment name."
}

variable "state_location" {
  type        = string
  description = "Absolute path of the directory that holds the OpenTofu state files."
}

variable "broker_principal" {
  type        = string
  description = "Not used on the local target."
  default     = ""
}

variable "ttl_hours" {
  type        = number
  description = "Lifetime of the environment in hours. 0 means no TTL."
  default     = 0
}
